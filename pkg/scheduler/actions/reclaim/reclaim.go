/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added job validation and preemption policy support
- Enhanced victim selection with priority queue ordering
- Added PrePredicate validation and node filtering

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reclaim

import (
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

type Action struct {
	enablePredicateErrorCache bool
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache: true,
	}
}

func (ra *Action) Name() string {
	return "reclaim"
}

func (ra *Action) Initialize() {}

func (ra *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, ra.Name())
	arguments.GetBool(&ra.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
}

func (ra *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Reclaim ...")
	defer klog.V(5).Infof("Leaving Reclaim ...")

	ra.parseArguments(ssn)

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}
	reclaimedByNode := make(map[string]*api.Resource)
	totalReclaimed := api.EmptyResource()
	creditedVictims := make(map[api.TaskID]struct{})

	klog.V(3).Infof("There are <%d> Jobs and <%d> Queues in total for scheduling.",
		len(ssn.Jobs), len(ssn.Queues))

	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip reclaim, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if queue, found := ssn.Queues[job.Queue]; !found {
			klog.Errorf("Failed to find Queue <%s> for Job <%s/%s>", job.Queue, job.Namespace, job.Name)
			continue
		} else if _, existed := queueMap[queue.UID]; !existed {
			klog.V(4).Infof("Added Queue <%s> for Job <%s/%s>", queue.Name, job.Namespace, job.Name)
			queueMap[queue.UID] = queue
			queues.Push(queue)
		}

		if ssn.JobStarving(job) {
			if _, found := preemptorsMap[job.Queue]; !found {
				preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			preemptorsMap[job.Queue].Push(job)
			preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				if task.SchGated {
					continue
				}
				preemptorTasks[job.UID].Push(task)
			}
		}
	}

	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)
		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		for {
			// Pick the starving jobs in this queue.
			jobsQ, found := preemptorsMap[queue.UID]
			if !found || jobsQ.Empty() {
				klog.V(4).Infof("No preemptors in Queue <%s>, break.", queue.Name)
				break
			}
			job := jobsQ.Pop().(*api.JobInfo)
			stmt := framework.NewStatement(ssn)
			jobReclaimedByNode := cloneReclaimedByNode(reclaimedByNode)
			jobTotalReclaimed := totalReclaimed.Clone()
			jobCreditedVictims := cloneCreditedVictims(creditedVictims)

			for {
				// If job is not request more resource, then stop reclaiming.
				if !ssn.JobStarving(job) {
					break
				}

				// Pick up all its candidate tasks.
				tasksQ, ok := preemptorTasks[job.UID]
				if !ok || tasksQ.Empty() {
					klog.V(3).Infof("No preemptor task in job <%s/%s>.",
						job.Namespace, job.Name)
					break
				}

				klog.V(3).Infof("Considering reclaim for %d tasks of job <%s/%s>.", tasksQ.Len(), job.Namespace, job.Name)

				task := tasksQ.Pop().(*api.TaskInfo)

				if task.Pod.Spec.PreemptionPolicy != nil && *task.Pod.Spec.PreemptionPolicy == v1.PreemptNever {
					klog.V(3).Infof("Task %s/%s cannot preempt (policy Never)", task.Namespace, task.Name)
					continue
				}

				if err := ssn.PrePredicateFn(task); err != nil {
					klog.V(3).Infof("PrePredicate failed for task %s/%s: %v", task.Namespace, task.Name, err)
					continue
				}

				if ra.pipelineWithIdleResources(ssn, stmt, queue, task) {
					continue
				}

				if !ssn.Preemptive(queue, []*api.TaskInfo{task}) {
					klog.V(3).Infof("Queue <%s> cannot reclaim for task <%s>, skip", queue.Name, task.Name)
					continue
				}

				ra.reclaimForTask(ssn, stmt, task, job, jobReclaimedByNode, jobTotalReclaimed, jobCreditedVictims)
			}

			if ssn.JobPipelined(job) {
				stmt.Commit()
				reclaimedByNode = jobReclaimedByNode
				totalReclaimed = jobTotalReclaimed
				creditedVictims = jobCreditedVictims
				klog.V(5).Infof("Committed reclaim credits after Job <%s/%s>: total credits <%v>, credited victims <%d>.",
					job.Namespace, job.Name, totalReclaimed, len(creditedVictims))
			} else {
				stmt.Discard()
				klog.V(5).Infof("Discarded reclaim credits from Job <%s/%s>: action-level total credits remain <%v>, credited victims <%d>.",
					job.Namespace, job.Name, totalReclaimed, len(creditedVictims))
			}

			if !jobsQ.Empty() {
				queues.Push(queue)
			}
		}
	}
}

// nodeVictimsInfo holds the reclaim information for a single node.
type nodeVictimsInfo struct {
	node               *api.NodeInfo
	victims            *util.PriorityQueue
	reclaimed          *api.Resource
	availableResources *api.Resource
}

func (ra *Action) pipelineWithIdleResources(
	ssn *framework.Session,
	stmt *framework.Statement,
	queue *api.QueueInfo,
	task *api.TaskInfo,
) bool {
	if !ssn.Allocatable(queue, task) {
		klog.V(5).Infof("Skip idle pipeline for Task <%s/%s> in Queue <%s>: queue is not allocatable for request <%v>.",
			task.Namespace, task.Name, queue.Name, task.InitResreq)
		return false
	}

	totalNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
	predicateHelper := util.NewPredicateHelper()
	predicateNodes, _ := predicateHelper.PredicateNodes(task, totalNodes, ssn.PredicateForPreemptAction, ra.enablePredicateErrorCache, ssn.NodesInShard)
	predicateNodesByShard := util.GetPredicatedNodeByShard(predicateNodes, ssn.NodesInShard)
	var idleCandidateNodes []*api.NodeInfo
	for _, nodes := range predicateNodesByShard {
		for _, node := range nodes {
			if !task.InitResreq.LessEqual(node.Idle, api.Zero) {
				klog.V(5).Infof("Skip idle pipeline for Task <%s/%s> on Node <%s>: requested <%v>, idle <%v>.",
					task.Namespace, task.Name, node.Name, task.InitResreq, node.Idle)
				continue
			}
			idleCandidateNodes = append(idleCandidateNodes, node)
		}
	}

	if len(idleCandidateNodes) == 0 {
		klog.V(5).Infof("No idle resource candidate found for Task <%s/%s>; checking reclaim eligibility.",
			task.Namespace, task.Name)
		return false
	}

	bestNode := idleCandidateNodes[0]
	if len(idleCandidateNodes) > 1 {
		nodeScores := util.PrioritizeNodes(task, idleCandidateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)
		bestNode = ssn.BestNodeFn(task, nodeScores)
		if bestNode == nil {
			bestNode, _ = util.SelectBestNodeAndScore(nodeScores)
		}
	}
	if bestNode == nil {
		klog.V(5).Infof("No best idle node selected for Task <%s/%s> from <%d> candidates; checking reclaim eligibility.",
			task.Namespace, task.Name, len(idleCandidateNodes))
		return false
	}

	nodeStmt := framework.NewStatement(ssn)
	if err := nodeStmt.Pipeline(task, bestNode.Name, false); err != nil {
		klog.V(5).Infof("Failed idle pipeline for Task <%s/%s> on Node <%s>: %v.",
			task.Namespace, task.Name, bestNode.Name, err)
		nodeStmt.DiscardWithReason("idle pipeline failed on node " + bestNode.Name)
		return false
	}

	stmt.Merge(nodeStmt)
	klog.V(3).Infof("Pipelined Task <%s/%s> on Node <%s> using idle resources <%v> without reclaim.",
		task.Namespace, task.Name, bestNode.Name, bestNode.Idle)
	return true
}

func (ra *Action) reclaimForTask(
	ssn *framework.Session,
	stmt *framework.Statement,
	task *api.TaskInfo,
	job *api.JobInfo,
	reclaimedByNode map[string]*api.Resource,
	totalReclaimed *api.Resource,
	creditedVictims map[api.TaskID]struct{},
) {
	if pipelineWithReclaimCredits(ssn, stmt, task, reclaimedByNode, totalReclaimed) {
		return
	}

	totalNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
	predicateHelper := util.NewPredicateHelper()
	predicateNodes, _ := predicateHelper.PredicateNodes(task, totalNodes, ssn.PredicateForPreemptAction, ra.enablePredicateErrorCache, ssn.NodesInShard)
	predicateNodesByShard := util.GetPredicatedNodeByShard(predicateNodes, ssn.NodesInShard)
	var predicateNodesByShardFlattened []*api.NodeInfo
	for _, nodes := range predicateNodesByShard {
		predicateNodesByShardFlattened = append(predicateNodesByShardFlattened, nodes...)
	}

	// Create the global allVictims priority queue using the same ordering as per-node queues
	allVictims := ssn.BuildAumovioVictimPriorityQueue(nil, task)

	// Map from victim UID to the node it belongs to
	victimToNode := make(map[api.TaskID]*api.NodeInfo)

	// Collect all possible victims for each node
	nodeVictimsMap := make(map[string]*nodeVictimsInfo)

	for _, n := range predicateNodesByShardFlattened {
		klog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.", task.Namespace, task.Name, n.Name)

		var reclaimees []*api.TaskInfo
		for _, taskOnNode := range n.Tasks {
			if taskOnNode.Status != api.Running || !taskOnNode.Preemptable {
				continue
			}

			if j, found := ssn.Jobs[taskOnNode.Job]; !found {
				continue
			} else if j.Queue != job.Queue {
				q := ssn.Queues[j.Queue]
				if !q.Reclaimable() {
					continue
				}
				reclaimees = append(reclaimees, taskOnNode.Clone())
			}
		}

		if len(reclaimees) == 0 {
			klog.V(4).Infof("No reclaimees on Node <%s>.", n.Name)
			continue
		}

		// Log all potential reclaimees before filtering
		reclaimeeNames := make([]string, 0, len(reclaimees))
		for _, v := range reclaimees {
			reclaimeeNames = append(reclaimeeNames, v.Namespace+"/"+v.Name)
		}
		klog.V(4).Infof("Potential reclaimees on Node <%s>: %v", n.Name, reclaimeeNames)

		victims := ssn.Reclaimable(task, reclaimees)

		victimNamesAfter := make([]string, 0, len(victims))
		for _, v := range victims {
			victimNamesAfter = append(victimNamesAfter, v.Namespace+"/"+v.Name)
		}
		klog.V(4).Infof("Victims after Reclaimable on Node <%s>: %v", n.Name, victimNamesAfter)

		if err := util.ValidateVictims(task, n, victims); err != nil {
			klog.V(3).Infof("No validated victims on Node <%s>: %v", n.Name, err)
			continue
		}

		// Build per-node victims priority queue
		nodeVictimsQueue := ssn.BuildAumovioVictimPriorityQueue(victims, task)

		// Store node info
		nodeVictimsMap[n.Name] = &nodeVictimsInfo{
			node:               n,
			victims:            nodeVictimsQueue,
			reclaimed:          api.EmptyResource(),
			availableResources: n.FutureIdle().Clone(),
		}

		// Push all victims to the global queue and track their node
		for _, victim := range victims {
			allVictims.Push(victim)
			victimToNode[victim.UID] = n
		}
	}

	// No victims found across all nodes
	if allVictims.Empty() {
		klog.V(3).Infof("No victims found for Task <%s/%s>.", task.Namespace, task.Name)
		return
	}

	// Set of nodes we've already tried and failed
	triedNodes := make(map[string]bool)

	// Try to reclaim from nodes based on the global victims priority
	for !allVictims.Empty() {
		// Pop the highest priority victim to determine which node to try
		initiatorVictim := allVictims.Pop().(*api.TaskInfo)
		victimNode := victimToNode[initiatorVictim.UID]

		// Log the initiator victim that triggered this node's reclaim attempt
		klog.V(3).Infof("Initiator victim <%s/%s> picked from allVictims queue, triggering reclaim attempt on Node <%s> for Task <%s/%s>.",
			initiatorVictim.Namespace, initiatorVictim.Name, victimNode.Name, task.Namespace, task.Name)

		// Skip if we've already tried this node
		if triedNodes[victimNode.Name] {
			klog.V(4).Infof("Node <%s> already tried, skipping.", victimNode.Name)
			continue
		}

		// Create a local statement for this node's eviction attempts
		nodeStmt := framework.NewStatement(ssn)

		// Clone the node's victims queue to iterate through
		nodeInfo := nodeVictimsMap[victimNode.Name]
		nodeVictimsQueue := nodeInfo.victims.Clone()
		reclaimed := nodeInfo.reclaimed.Clone()
		availableResources := nodeInfo.availableResources.Clone()
		evictionFailed := false
		evictionOccurred := false
		taskCanBePipelined := false
		attemptReclaimedByNode := make(map[string]*api.Resource)
		attemptTotalReclaimed := api.EmptyResource()
		attemptCreditedVictims := make(map[api.TaskID]struct{})

		for !nodeVictimsQueue.Empty() {
			victim := nodeVictimsQueue.Pop().(*api.TaskInfo)
			klog.V(3).Infof("Try to reclaim Task <%s/%s> for Tasks <%s/%s> on Node <%s>",
				victim.Namespace, victim.Name, task.Namespace, task.Name, victimNode.Name)

			if err := nodeStmt.Evict(victim, "reclaim"); err != nil {
				klog.Errorf("Failed to reclaim Task <%s/%s> for Task <%s/%s> on Node <%s>: %v",
					victim.Namespace, victim.Name, task.Namespace, task.Name, victimNode.Name, err)
				evictionFailed = true
				break
			}

			reclaimed.Add(victim.Resreq)
			availableResources.Add(victim.Resreq)
			evictionOccurred = true
			accumulateReclaimCreditsFromVictim(ssn, victim, attemptReclaimedByNode, attemptTotalReclaimed, attemptCreditedVictims, creditedVictims)

			klog.V(3).Infof("Reclaimed <%v/%v> for task <%s/%s> requested <%v> on "+
				"Node <%s> with availableResources <%v> and reclaimed <%v>.",
				victim.Namespace, victim.Name, task.Namespace, task.Name, task.InitResreq,
				victimNode.Name, availableResources, reclaimed)

			if task.InitResreq.LessEqual(availableResources, api.Zero) {
				taskCanBePipelined = true
				break
			}
		}
		triedNodes[victimNode.Name] = true

		// If any eviction failed, discard all evictions for this node and try next
		if evictionFailed {
			klog.V(3).Infof("Eviction failed on Node <%s>, discarding all evictions and trying next node.", victimNode.Name)
			nodeStmt.DiscardWithReason("eviction failed on node " + victimNode.Name)
			continue
		}

		// Check if we have enough resources after all evictions
		if !taskCanBePipelined {
			klog.V(3).Infof("Not enough resources on Node <%s> after reclaiming (reclaimed: %v, available: %v, required: %v), discarding and trying next node.",
				victimNode.Name, reclaimed, availableResources, task.InitResreq)
			nodeStmt.DiscardWithReason("insufficient resources on node " + victimNode.Name)
			continue
		}

		// Try to pipeline the task to this node
		if err := nodeStmt.Pipeline(task, victimNode.Name, evictionOccurred); err != nil {
			klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>: %v",
				task.Namespace, task.Name, victimNode.Name, err)
			nodeStmt.DiscardWithReason("pipeline failed on node " + victimNode.Name)
			continue
		}

		// Success: transfer ownership of nodeStmt's operations to the outer stmt.
		// nodeStmt's effects (evictions, pipeline) are already live in the session.
		stmt.Merge(nodeStmt)
		mergeReclaimCredits(reclaimedByNode, totalReclaimed, creditedVictims, attemptReclaimedByNode, attemptTotalReclaimed, attemptCreditedVictims)
		klog.V(3).Infof("Successfully pipelined Task <%s/%s> on Node <%s>, reclaimed: <%v>.",
			task.Namespace, task.Name, victimNode.Name, reclaimed)
		return
	}
	klog.V(3).Infof("Failed to reclaim resources for Task <%s/%s> on any node.", task.Namespace, task.Name)
}

func hasSufficientReclaimCredits(
	task *api.TaskInfo,
	nodeName string,
	reclaimedByNode map[string]*api.Resource,
	totalReclaimed *api.Resource,
) bool {
	nodeCredits, found := reclaimedByNode[nodeName]
	if !found {
		return false
	}

	if !task.InitResreq.LessEqual(totalReclaimed, api.Zero) {
		return false
	}

	return task.InitResreq.LessEqual(nodeCredits, api.Zero)
}

func pipelineWithReclaimCredits(
	ssn *framework.Session,
	stmt *framework.Statement,
	task *api.TaskInfo,
	reclaimedByNode map[string]*api.Resource,
	totalReclaimed *api.Resource,
) bool {
	if !task.InitResreq.LessEqual(totalReclaimed, api.Zero) {
		klog.V(5).Infof("Skip reclaim-credit pipeline for Task <%s/%s>: requested <%v>, total reclaim credits <%v>.",
			task.Namespace, task.Name, task.InitResreq, totalReclaimed)
		return false
	}

	for _, nodeName := range reclaimCreditNodeNames(reclaimedByNode) {
		node, found := ssn.Nodes[nodeName]
		if !found {
			klog.V(5).Infof("Skip reclaim-credit pipeline for Task <%s/%s> on Node <%s>: node not found in session.",
				task.Namespace, task.Name, nodeName)
			continue
		}

		if options.ServerOpts.ShardingMode == commonutil.HardShardingMode && !ssn.NodesInShard.Has(nodeName) {
			klog.V(5).Infof("Skip reclaim-credit pipeline for Task <%s/%s> on Node <%s>: node is outside hard scheduler shard.",
				task.Namespace, task.Name, nodeName)
			continue
		}

		if !hasSufficientReclaimCredits(task, nodeName, reclaimedByNode, totalReclaimed) {
			klog.V(5).Infof("Skip reclaim-credit pipeline for Task <%s/%s> on Node <%s>: requested <%v>, node credits <%v>, total credits <%v>.",
				task.Namespace, task.Name, nodeName, task.InitResreq, reclaimedByNode[nodeName], totalReclaimed)
			continue
		}

		futureIdle := node.FutureIdle()
		if !task.InitResreq.LessEqual(futureIdle, api.Zero) {
			klog.V(5).Infof("Skip reclaim-credit pipeline for Task <%s/%s> on Node <%s>: requested <%v>, future idle <%v>, credits <%v>.",
				task.Namespace, task.Name, nodeName, task.InitResreq, futureIdle, reclaimedByNode[nodeName])
			continue
		}

		if err := ssn.PredicateForPreemptAction(task, node); err != nil {
			klog.V(5).Infof("Skip reclaim-credit pipeline for Task <%s/%s> on Node <%s>: predicate failed: %v.",
				task.Namespace, task.Name, nodeName, err)
			continue
		}

		nodeStmt := framework.NewStatement(ssn)
		if err := nodeStmt.Pipeline(task, nodeName, true); err != nil {
			klog.V(5).Infof("Failed reclaim-credit pipeline for Task <%s/%s> on Node <%s>: %v.",
				task.Namespace, task.Name, nodeName, err)
			nodeStmt.DiscardWithReason("reclaim-credit pipeline failed on node " + nodeName)
			continue
		}

		consumeReclaimCredits(task, nodeName, reclaimedByNode, totalReclaimed)
		stmt.Merge(nodeStmt)
		klog.V(3).Infof("Directly pipelined Task <%s/%s> on Node <%s> using reclaim credits and FutureIdle <%v>.",
			task.Namespace, task.Name, nodeName, futureIdle)
		klog.V(5).Infof("Consumed reclaim credits for Task <%s/%s> on Node <%s>: remaining node credits <%v>, total credits <%v>.",
			task.Namespace, task.Name, nodeName, reclaimedByNode[nodeName], totalReclaimed)
		return true
	}

	klog.V(5).Infof("No reclaim-credit pipeline candidate succeeded for Task <%s/%s>; falling back to victim discovery.",
		task.Namespace, task.Name)
	return false
}

func reclaimCreditNodeNames(reclaimedByNode map[string]*api.Resource) []string {
	nodeNames := make([]string, 0, len(reclaimedByNode))
	for nodeName, credits := range reclaimedByNode {
		if credits == nil || credits.IsEmpty() {
			continue
		}
		nodeNames = append(nodeNames, nodeName)
	}
	sort.Strings(nodeNames)
	return nodeNames
}

func cloneReclaimedByNode(reclaimedByNode map[string]*api.Resource) map[string]*api.Resource {
	clone := make(map[string]*api.Resource, len(reclaimedByNode))
	for nodeName, credits := range reclaimedByNode {
		if credits == nil {
			clone[nodeName] = nil
			continue
		}
		clone[nodeName] = credits.Clone()
	}
	return clone
}

func cloneCreditedVictims(creditedVictims map[api.TaskID]struct{}) map[api.TaskID]struct{} {
	clone := make(map[api.TaskID]struct{}, len(creditedVictims))
	for victimID := range creditedVictims {
		clone[victimID] = struct{}{}
	}
	return clone
}

func consumeReclaimCredits(
	task *api.TaskInfo,
	nodeName string,
	reclaimedByNode map[string]*api.Resource,
	totalReclaimed *api.Resource,
) {
	if !hasSufficientReclaimCredits(task, nodeName, reclaimedByNode, totalReclaimed) {
		return
	}

	reclaimedByNode[nodeName].Sub(task.InitResreq)
	totalReclaimed.Sub(task.InitResreq)
}

func accumulateReclaimCreditsFromVictim(
	ssn *framework.Session,
	victim *api.TaskInfo,
	attemptReclaimedByNode map[string]*api.Resource,
	attemptTotalReclaimed *api.Resource,
	attemptCreditedVictims map[api.TaskID]struct{},
	creditedVictims map[api.TaskID]struct{},
) {
	candidates := []*api.TaskInfo{victim}

	if policy, ok := victim.Pod.Annotations[framework.GroupEvictionPolicyAnnotationKey]; ok && policy == "minMember" {
		if victimJob, found := ssn.Jobs[victim.Job]; found {
			for _, groupTask := range victimJob.Tasks {
				if groupTask.UID != victim.UID {
					candidates = append(candidates, groupTask)
				}
			}
		}
	}

	for _, candidate := range candidates {
		if _, found := creditedVictims[candidate.UID]; found {
			continue
		}
		if _, found := attemptCreditedVictims[candidate.UID]; found {
			continue
		}
		if candidate.Status != api.Releasing {
			continue
		}
		if len(candidate.NodeName) == 0 {
			continue
		}

		if _, found := attemptReclaimedByNode[candidate.NodeName]; !found {
			attemptReclaimedByNode[candidate.NodeName] = api.EmptyResource()
		}
		attemptReclaimedByNode[candidate.NodeName].Add(candidate.Resreq)
		attemptTotalReclaimed.Add(candidate.Resreq)
		attemptCreditedVictims[candidate.UID] = struct{}{}
	}
}

func mergeReclaimCredits(
	reclaimedByNode map[string]*api.Resource,
	totalReclaimed *api.Resource,
	creditedVictims map[api.TaskID]struct{},
	attemptReclaimedByNode map[string]*api.Resource,
	attemptTotalReclaimed *api.Resource,
	attemptCreditedVictims map[api.TaskID]struct{},
) {
	for nodeName, res := range attemptReclaimedByNode {
		if _, found := reclaimedByNode[nodeName]; !found {
			reclaimedByNode[nodeName] = api.EmptyResource()
		}
		reclaimedByNode[nodeName].Add(res)
	}

	totalReclaimed.Add(attemptTotalReclaimed)

	for victimID := range attemptCreditedVictims {
		creditedVictims[victimID] = struct{}{}
	}
}

func (ra *Action) UnInitialize() {
}
