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
				var dims *api.ResourceNameList
				if ok, dims = ssn.Preemptive(queue, task); !ok {
					klog.V(3).Infof("Queue <%s> cannot reclaim for task <%s>, skip", queue.Name, task.Name)
					continue
				}

				if err := ssn.PrePredicateFn(task); err != nil {
					klog.V(3).Infof("PrePredicate failed for task %s/%s: %v", task.Namespace, task.Name, err)
					continue
				}

				ra.reclaimForTask(ssn, stmt, task, dims, job)
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

func (ra *Action) reclaimForTask(ssn *framework.Session, stmt *framework.Statement, task *api.TaskInfo, job *api.JobInfo) {
// nodeBuckets holds priority queues of nodes grouped by dimension match count
type nodeBuckets struct {
	buckets       map[int]*util.PriorityQueue
	maxMatchCount int
}

func (ra *Action) reclaimForTask(ssn *framework.Session, stmt *framework.Statement, task *api.TaskInfo, dims *api.ResourceNameList, job *api.JobInfo) {
	totalNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
	predicateHelper := util.NewPredicateHelper()
	predicateNodes, _ := predicateHelper.PredicateNodes(task, totalNodes, ssn.PredicateForPreemptAction, ra.enablePredicateErrorCache, ssn.NodesInShard)
	predicateNodesByShard := util.GetPredicatedNodeByShard(predicateNodes, ssn.NodesInShard)
	var predicateNodesByShardFlattened []*api.NodeInfo
	for _, nodes := range predicateNodesByShard {
		predicateNodesByShardFlattened = append(predicateNodesByShardFlattened, nodes...)
	}

	// Prioritize nodes by dimension match into buckets
	nodeBuckets := ra.prioritizeNodesByDimensionMatch(predicateNodesByShardFlattened, dims, task)

    // Process nodes bucket by bucket (highest match count first)
    for i := nodeBuckets.maxMatchCount; i >= 0; i-- {
        bucket, exists := nodeBuckets.buckets[i]
        if !exists {
            continue
        }

		klog.V(3).Infof("Processing bucket with %d dimension matches (%d nodes) for Task <%s/%s>",
            i, bucket.Len(), task.Namespace, task.Name)


        // Process all nodes in this bucket (they're already in priority order)
        for !bucket.Empty() {
            n := bucket.Pop().(*api.NodeInfo)
            klog.V(3).Infof("Considering Task <%s/%s> on Node <%s> with FutureIdle <%v>.",
                task.Namespace, task.Name, n.Name, n.FutureIdle())

            // Build priority queue of reclaimees on this node
            reclaimeesQueue := util.NewPriorityQueue(func(l, r interface{}) bool {
                return ssn.VictimQueueAndTaskOrderFn(l, r, task)
            })
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
                    reclaimeesQueue.Push(taskOnNode.Clone())
                    reclaimees = append(reclaimees, taskOnNode.Clone())
                }
            }

            if reclaimeesQueue.Empty() {
                klog.V(4).Infof("No reclaimees on Node <%s>.", n.Name)
                continue
            }

            reclaimees := make([]*api.TaskInfo, 0, reclaimeesQueue.Len())
            for !reclaimeesQueue.Empty() {
                reclaimees = append(reclaimees, reclaimeesQueue.Pop().(*api.TaskInfo))
            }
			victims := ssn.Reclaimable(task, reclaimees)
			if err := util.ValidateVictims(task, n, victims); err != nil {
				klog.V(3).Infof("No validated victims on Node <%s>: %v", n.Name, err)
				continue
			}

			victimsQueue := ssn.BuildAumovioVictimPriorityQueue(victims, task)
			resreq := task.InitResreq.Clone()
			reclaimed := api.EmptyResource()

			// The reclaimed resources should be added to the remaining available resources of the nodes to avoid over-reclaiming.
			availableResources := n.FutureIdle()

			evictionOccurred := false
			for !victimsQueue.Empty() {
				reclaimee := victimsQueue.Pop().(*api.TaskInfo)
				klog.V(3).Infof("Try to reclaim Task <%s/%s> for Tasks <%s/%s>",
					reclaimee.Namespace, reclaimee.Name, task.Namespace, task.Name)
				if err := stmt.Evict(reclaimee, "reclaim"); err != nil {
					klog.Errorf("Failed to reclaim Task <%s/%s> for Tasks <%s/%s>: %v",
						reclaimee.Namespace, reclaimee.Name, task.Namespace, task.Name, err)
					continue
				}
				reclaimed.Add(reclaimee.Resreq)
				availableResources.Add(reclaimee.Resreq)
				evictionOccurred = true
				if resreq.LessEqual(availableResources, api.Zero) {
					break
				}
			}

			klog.V(3).Infof("Reclaimed <%v> for task <%s/%s> requested <%v>, and Node <%s> availableResources <%v>.", reclaimed, task.Namespace, task.Name, task.InitResreq, n.Name, availableResources)

			if task.InitResreq.LessEqual(availableResources, api.Zero) {
				if err := stmt.Pipeline(task, n.Name, evictionOccurred); err != nil {
					klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
						task.Namespace, task.Name, n.Name)
					if rollbackErr := stmt.UnPipeline(task); rollbackErr != nil {
						klog.Errorf("Failed to unpipeline Task %v on %v in Session %v for %v.",
							task.UID, n.Name, ssn.UID, rollbackErr)
					}
				}
				break
			}
		}
	}
}

func (ra *Action) UnInitialize() {
}

// prioritizeNodesByDimensionMatch creates buckets of nodes based on dimension matching.
// Within each bucket, nodes are sorted by their resource availability in those dimensions.
// Returns a nodeBuckets struct containing priority queues ordered by match count.
func (ra *Action) prioritizeNodesByDimensionMatch(nodes []*api.NodeInfo, dims *api.ResourceNameList, task *api.TaskInfo) *nodeBuckets {
	buckets := &nodeBuckets{
		buckets:       make(map[int]*util.PriorityQueue),
		maxMatchCount: 0,
	}

	if dims == nil || len(*dims) == 0 {
		// Create a single bucket with all nodes
		buckets.buckets[0] = util.NewPriorityQueue(func(l, r interface{}) bool { return true })
		for _, node := range nodes {
			buckets.buckets[0].Push(node)
		}
		return buckets
	}

	// Create a LessFn that prioritizes nodes with more resources in the specified dimensions
	nodeScoreLessFn := func(l, r interface{}) bool {
		ln := l.(*api.NodeInfo)
		rn := r.(*api.NodeInfo)

		// Calculate total available resources in the specified dimensions
		lScore := 0.0
		rScore := 0.0

		for _, dimName := range *dims {
			lScore += ln.FutureIdle().Get(dimName)
			rScore += rn.FutureIdle().Get(dimName)
		}

		// Higher score should come first (return true if l has higher score)
		return lScore > rScore
	}

	// Create a deficit-based LessFn for the zero-match bucket
	deficitLessFn := func(l, r interface{}) bool {
		ln := l.(*api.NodeInfo)
		rn := r.(*api.NodeInfo)

		// Calculate deficit: how much more resource is needed to fit the task
		lDeficit := api.ExceededPart(task.InitResreq, ln.FutureIdle())
		rDeficit := api.ExceededPart(task.InitResreq, rn.FutureIdle())

		// Compare total deficit - we want the smallest deficit first
		lTotal := lDeficit.MilliCPU + lDeficit.Memory
		rTotal := rDeficit.MilliCPU + rDeficit.Memory

		for _, v := range lDeficit.ScalarResources {
			lTotal += v
		}
		for _, v := range rDeficit.ScalarResources {
			rTotal += v
		}

		return lTotal < rTotal
	}

	for _, node := range nodes {
		futureIdle := node.FutureIdle()
		matchCount := 0

		// Count how many dimensions from dims are present in FutureIdle
		for _, dimName := range *dims {
			if quantity := futureIdle.Get(dimName); quantity > api.GetMinResource() {
				matchCount++
			}
		}

		// Add node to appropriate bucket
		if _, exists := buckets.buckets[matchCount]; !exists {
			// Use deficit-based ordering for zero-match bucket, score-based for others
			if matchCount == 0 {
				buckets.buckets[matchCount] = util.NewPriorityQueue(deficitLessFn)
			} else {
				buckets.buckets[matchCount] = util.NewPriorityQueue(nodeScoreLessFn)
			}
		}

		buckets.buckets[matchCount].Push(node)

		if matchCount > buckets.maxMatchCount {
			buckets.maxMatchCount = matchCount
		}
	}

	return buckets
}
