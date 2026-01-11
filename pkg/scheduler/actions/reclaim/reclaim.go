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
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
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

				if !ssn.Preemptive(queue, task) {
					klog.V(3).Infof("Queue <%s> cannot reclaim for task <%s>, skip", queue.Name, task.Name)
					continue
				}

				if err := ssn.PrePredicateFn(task); err != nil {
					klog.V(3).Infof("PrePredicate failed for task %s/%s: %v", task.Namespace, task.Name, err)
					continue
				}

				ra.reclaimForTask(ssn, stmt, task, job)
			}

			if ssn.JobPipelined(job) {
				stmt.Commit()
			} else {
				stmt.Discard()
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

func (ra *Action) reclaimForTask(ssn *framework.Session, stmt *framework.Statement, task *api.TaskInfo, job *api.JobInfo) {
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

		victims := ssn.Reclaimable(task, reclaimees)

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

	// Save the original statement operations before trying any node
	// This allows us to restore the original state if all node attempts fail
	savedOriginalStmt := framework.SaveOperations(stmt)

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
			nodeStmt.Discard()
			continue
		}

		// Check if we have enough resources after all evictions
		if !taskCanBePipelined {
			klog.V(3).Infof("Not enough resources on Node <%s> after reclaiming (reclaimed: %v, available: %v, required: %v), discarding and trying next node.",
				victimNode.Name, reclaimed, availableResources, task.InitResreq)
			nodeStmt.Discard()
			continue
		}

		// Try to pipeline the task to this node
		if err := nodeStmt.Pipeline(task, victimNode.Name, evictionOccurred); err != nil {
			klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>: %v",
				task.Namespace, task.Name, victimNode.Name, err)
			nodeStmt.Discard()
			continue
		}
		mergedStmt := framework.SaveOperations(savedOriginalStmt, nodeStmt)
		nodeStmt.Discard()

		if err := stmt.RecoverOperations(mergedStmt); err != nil {
			klog.Errorf("Failed to Save merged statements: %v", err)
			// Try next node if merging fails
			stmt.Discard()
			if err := stmt.RecoverOperations(savedOriginalStmt); err != nil {
				klog.Errorf("Failed to recover original statement operations: %v", err)
				// This is a critical error, we cannot proceed
				return
			}
			// We still have hope let's continue
			continue
		}
		klog.V(3).Infof("Successfully reclaimed and pipelined Task <%s/%s> on Node <%s>, reclaimed: <%v>.",
			task.Namespace, task.Name, victimNode.Name, reclaimed)
		return
	}
	klog.V(3).Infof("Failed to reclaim resources for Task <%s/%s> on any node.", task.Namespace, task.Name)
}

func (ra *Action) UnInitialize() {
}
