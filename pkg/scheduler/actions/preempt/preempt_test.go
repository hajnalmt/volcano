/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Rewritten tests using TestCommonStruct framework with comprehensive preemption scenarios
- Added TestTopologyAwarePreempt for topology-aware preemption testing

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

package preempt

import (
	"flag"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	options.Default()
	klog.InitFlags(nil)
	flag.Set("v", "4")
}

func TestPreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	tests := []uthelper.TestCommonStruct{
		{
			Name: "do not preempt if there are enough idle resources",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 3, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// If there are enough idle resources on the node, then there is no need to preempt anything.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "do not preempt if job is pipelined",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
			},
			// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			// All resources on the node will be in use.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "preempt one task of different job to fit both jobs on one node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preempt enough tasks to fit large task of different job",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
			// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("5", "5G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee1"},
			ExpectEvictNum: 2,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue: allocatable and has enough resource, don't preempt",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvictNum: 0,
		},
		{
			// case about issue #2232
			Name: "preempt low priority job in same queue but not pod with preemptable=false or higher priority",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPriority("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &highPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("3", "3G")),
			},
			ExpectEvicted:  []string{"c1/preemptee2"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "unBestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "BestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, v1.ResourceList{}, "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3642
			Name: "can not preempt resources when task preemption policy is never",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptNever),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
		{
			// Verify that evictions are rolled back when the preemptor cannot be
			// allocated due to queue capacity limits. The node has plenty of idle
			// resources (10 CPU), and victims are preemptable, so evictions will be
			// attempted inside a temporary nodeStmt. However the preemptor requests
			// 4 CPU while the queue capacity is only 3 CPU, so the Allocatable
			// check fails after all victims have been evicted. The temporary
			// statement is discarded and none of the evictions are committed.
			Name: "rollback evictions when preemptor exceeds queue capacity after evicting victims",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("4", "4G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("3", "3G")),
			},
			ExpectEvictNum: 0,
		},
		{
			// Verify that when preemption succeeds on n2 after failing on n1, only n2's
			// victims are committed — n1's evictions must be discarded by nodeStmt.Discard().
			//
			// n1 victim (1 CPU): after eviction queue.Allocated drops from 5 to 4 CPU;
			// 4 + 3 (preemptor) = 7 > 6 (cap) → Allocatable fails → nodeStmt discarded.
			// n2 victim (4 CPU): after eviction queue.Allocated drops from 5 to 1 CPU;
			// 1 + 3 = 4 ≤ 6 → Allocatable passes → pipeline succeeds → nodeStmt merged.
			//
			// Without the per-node nodeStmt isolation, n1's victim would be committed
			// alongside n2's because the single shared statement would not be rolled back.
			Name: "only commit evictions on the node where preemption succeeds",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
			},
			Pods: []*v1.Pod{
				// n1 victim: small — freeing 1 CPU is not enough to satisfy Allocatable
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				// n2 victim: large — freeing 4 CPU brings queue.Allocated within cap
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("4", "4G"), "pg3", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			// Cluster: 7 CPU total. Queue cap: 6 CPU → deserved = 6 CPU.
			// n1 has 2 CPU idle so ValidateVictims passes (2 idle + 1 victim = 3 ≥ 3).
			// n2 has 0 CPU idle; ValidateVictims passes via victim alone (4 ≥ 3).
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"},
		},
		{
			// Regression test for the preemptorTasks overwrite issue in multi-queue preemption.
			//
			// Instead of:
			//    intraJobPreemptors := util.NewPriorityQueue(ssn.TaskOrderFn)
			// We have used:
			//    preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			// in the "Preemption between Task within Job" loop, which caused preemptorTasks to be overwritten/drained across queues.
			// This test verifies that the preemptorTasks for pg3 (high-priority preemptor in q2) is not overwritten/drained when processing q1, so that pg3 can successfully preempt pg2.
			//
			// Scenario:
			// - q1 has a running non-starving job (pg1) and no preemptor.
			// - q2 has a low-priority running victim (pg2) and a high-priority starving
			//   preemptor job (pg3).
			// - underRequest is shared across queues.
			//
			// Buggy behavior:
			// - While processing q1, the intra-job pass overwrites/drains
			//   preemptorTasks[pg3], so q2 later sees no preemptor and skips eviction.
			//
			// Why this was flaky:
			// - Queue iteration order came from a Go map, so the run usually passed when
			//   q2 was visited first, but failed when q1 was visited first.
			Name: "multi-queue: preemptorTasks must not be overwritten by intra-job preemption of another queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q2", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "q1-runner1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "q2-preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "q2-preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/q2-preemptee1"},
			ExpectEvictNum: 1,
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledOverused:    &trueValue,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[0].Name(),
				Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: false}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTopologyAwarePreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		capacity.PluginName:    capacity.New,
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
		predicates.PluginName:  predicates.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	tests := []uthelper.TestCommonStruct{
		{
			Name: "do not preempt if there are enough idle resources",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 3, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// If there are enough idle resources on the node, then there is no need to preempt anything.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "do not preempt if job is pipelined",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
			},
			// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			// All resources on the node will be in use.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "preempt one task of different job to fit both jobs on one node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preempt enough tasks to fit large task of different job",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
			// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("5", "5G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee1"},
			ExpectEvictNum: 2,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue: allocatable and has enough resource, don't preempt",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvictNum: 0,
		},
		{
			// case about issue #2232
			Name: "preempt low priority job in same queue but not pod with preemptable=false or higher priority",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPriority("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &highPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("3", "3G")),
			},
			ExpectEvicted:  []string{"c1/preemptee2"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "unBestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "BestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, v1.ResourceList{}, "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3642
			Name: "can not preempt resources when task preemption policy is never",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptNever),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
		{
			Name: "preempt low-priority pod when high-priority pod has PodAntiAffinity conflict",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				buildPodWithPodAntiAffinity("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true", "test": "test"}, make(map[string]string), "kubernetes.io/hostname"),

				buildPodWithPodAntiAffinity("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", map[string]string{"test": "test"}, make(map[string]string), "kubernetes.io/hostname"),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), map[string]string{"kubernetes.io/hostname": "n1"}),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("2", "2G")),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"},
		},
		{
			Name: "preemption with priority queues",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg3", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning, "high-priority"),
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg3", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptLowerPriority),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueueWithPriorityAndResourcesQuantity("q1", 1, api.BuildResourceList("1", "1G"), api.BuildResourceList("1", "1G")),
				util.BuildQueueWithPriorityAndResourcesQuantity("q2", 10, api.BuildResourceList("1", "1G"), api.BuildResourceList("1", "1G")),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledOverused:    &trueValue,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnabledPredicate:   &trueValue,
				},
				{
					Name:               predicates.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledPredicate:   &trueValue,
				},
				{
					Name:              capacity.PluginName,
					EnabledQueueOrder: &trueValue,
				},
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[0].Name(),
				Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: true}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestPreemptCreditFastPathReusesFreedResources verifies the session-level
// eviction credit flow added for preempt in #9. A single low-priority victim
// holds both MIG slices on the node. A high-priority gang (minMember 2) needs
// one MIG slice per worker. Evicting the single victim frees two MIG slices, so
// the first worker is pipelined via a normal eviction (which accumulates the
// freed resources as session-level eviction credits) and the second worker is
// pipelined via the credit fast path (pipelineWithEvictionCredits) without a
// second eviction.
//
// The queue is capped at a single MIG slice on purpose: after the first worker
// is pipelined the queue is over its MIG capability, so ssn.Allocatable is false
// for the second worker and normalPreempt cannot place it via its FutureIdle
// path (that path is gated on Allocatable). Only the credit fast path, which
// bypasses Allocatable because the eviction is already committed, can pipeline
// the second worker. This isolates the credit flow: disabling
// pipelineWithEvictionCredits leaves the gang with a single pipelined worker and
// fails the Pipelined==2 expectation.
func TestPreemptCreditFastPathReusesFreedResources(t *testing.T) {
	const migResource = "nvidia.com/mig-3g.40gb"

	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	// One low-priority victim holds both MIG slices on n1.
	victim := util.BuildPod("c1", "victim-big", "n1", v1.PodRunning,
		api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: migResource, Value: "2"}}...),
		"pg-victim", map[string]string{"app": "victim"}, make(map[string]string))
	victim.Annotations[schedulingv1beta1.PodPreemptable] = "true"

	// A high-priority gang (minMember 2); each worker needs one MIG slice.
	worker1 := util.BuildPod("c1", "worker-1", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-preemptor", map[string]string{"app": "mpi"}, make(map[string]string))
	worker2 := util.BuildPod("c1", "worker-2", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-preemptor", map[string]string{"app": "mpi"}, make(map[string]string))

	test := &uthelper.TestCommonStruct{
		Name: "preempt reuses freed resources via eviction credits",
		Plugins: map[string]framework.PluginBuilder{
			capacity.PluginName:    capacity.New,
			conformance.PluginName: conformance.New,
			gang.PluginName:        gang.New,
			priority.PluginName:    priority.New,
			predicates.PluginName:  predicates.New,
		},
		PriClass: []*schedulingv1.PriorityClass{highPrio, lowPrio},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithPrio("pg-victim", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
			util.BuildPodGroupWithPrio("pg-preemptor", "c1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
		},
		Pods: []*v1.Pod{victim, worker1, worker2},
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: migResource, Value: "2"}, {Name: "pods", Value: "10"}}...), map[string]string{v1.LabelHostname: "n1"}),
		},
		Queues: []*schedulingv1beta1.Queue{
			// Capability grants a single MIG slice, keeping the queue over
			// capacity for the second worker so only the credit fast path applies.
			util.BuildQueueWithResourcesQuantity("q1",
				api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "10"}}...),
				api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "10"}}...)),
		},
		// Only one eviction happens; the freed MIG slices cover both workers.
		ExpectEvicted:  []string{"c1/victim-big"},
		ExpectEvictNum: 1,
		ExpectPipeLined: map[string][]string{
			"c1/pg-preemptor": {"n1"},
		},
		ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
			"c1/pg-preemptor": {
				api.Pipelined: 2,
			},
		},
	}

	trueValue := true
	// gang is enabled only for pipelined/starving of the preemptor gang, not for
	// Preemptable, so it does not veto evicting the lone minMember-1 victim.
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               capacity.PluginName,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	test.RegisterSession(tiers, []conf.Configuration{{Name: New().Name(),
		Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: false}}})
	defer test.Close()
	test.Run([]framework.Action{New()})
	if err := test.CheckAll(0); err != nil {
		t.Fatal(err)
	}
}

// TestPreemptGroupEvictionPolicyCreditsAcrossNodes is the preempt analog of the
// reclaim MPI test (TestReclaimPipelinesMPILauncherWithIdleResources). It shows
// that preempt does NOT account for resources freed by the
// volcano.sh/group-eviction-policy=minMember cascade, whereas reclaim does.
//
// Setup: a low-priority victim gang (minMember 2) holds one MIG slice on each of
// two nodes; both victim pods carry group-eviction-policy=minMember. A
// high-priority gang (minMember 2) needs one MIG slice per worker. The queue is
// capped at a single MIG slice so that, after the first worker is pipelined, the
// queue is over capacity (ssn.Allocatable is false) for the second worker.
//
// Expected correct behaviour: evicting victim-1 on n1 for worker-1 cascades to
// evict victim-2 on n2, freeing a MIG slice there. worker-2 (not allocatable, and
// unable to re-evict the already-Releasing victim-2) can then only be pipelined
// on n2 via the credit fast path, using credit that must be recorded for n2 from
// the cascade. Both workers pipeline, exactly one direct eviction plus one
// cascade eviction occur (ExpectEvictNum 2).
//
// Actual behaviour today: preempt credits only the on-node victim it selected
// (victim-1 on n1) and never records the cascade-freed victim-2 on n2, so worker-2
// cannot be pipelined, the gang rolls back, and the test fails. This test is
// expected to FAIL until preempt shares reclaim's group-eviction-policy aware
// credit accounting.
func TestPreemptGroupEvictionPolicyCreditsAcrossNodes(t *testing.T) {
	const migResource = "nvidia.com/mig-3g.40gb"

	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	// Low-priority victim gang: one MIG slice on each node, group-evicted together.
	victim1 := util.BuildPod("c1", "victim-1", "n1", v1.PodRunning,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-victim", map[string]string{"app": "victim"}, make(map[string]string))
	victim2 := util.BuildPod("c1", "victim-2", "n2", v1.PodRunning,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-victim", map[string]string{"app": "victim"}, make(map[string]string))
	for _, pod := range []*v1.Pod{victim1, victim2} {
		pod.Annotations[schedulingv1beta1.PodPreemptable] = "true"
		pod.Annotations[framework.GroupEvictionPolicyAnnotationKey] = "minMember"
	}

	// High-priority gang (minMember 2); each worker needs one MIG slice.
	worker1 := util.BuildPod("c1", "worker-1", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-preemptor", map[string]string{"app": "mpi"}, make(map[string]string))
	worker2 := util.BuildPod("c1", "worker-2", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-preemptor", map[string]string{"app": "mpi"}, make(map[string]string))

	test := &uthelper.TestCommonStruct{
		Name: "preempt reuses group-eviction cascade resources via credits",
		Plugins: map[string]framework.PluginBuilder{
			capacity.PluginName:    capacity.New,
			conformance.PluginName: conformance.New,
			gang.PluginName:        gang.New,
			priority.PluginName:    priority.New,
			predicates.PluginName:  predicates.New,
		},
		PriClass: []*schedulingv1.PriorityClass{highPrio, lowPrio},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithPrio("pg-victim", "c1", "q1", 2, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
			util.BuildPodGroupWithPrio("pg-preemptor", "c1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
		},
		Pods: []*v1.Pod{victim1, victim2, worker1, worker2},
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "10"}}...), map[string]string{v1.LabelHostname: "n1"}),
			util.BuildNode("n2", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "10"}}...), map[string]string{v1.LabelHostname: "n2"}),
		},
		Queues: []*schedulingv1beta1.Queue{
			// Capability grants a single MIG slice, keeping the queue over
			// capacity for the second worker so only the credit fast path applies.
			util.BuildQueueWithResourcesQuantity("q1",
				api.BuildResourceList("16", "16Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "20"}}...),
				api.BuildResourceList("16", "16Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "20"}}...)),
		},
		// One direct eviction (victim-1) plus one cascade eviction (victim-2).
		ExpectEvicted:  []string{"c1/victim-1", "c1/victim-2"},
		ExpectEvictNum: 2,
		ExpectPipeLined: map[string][]string{
			"c1/pg-preemptor": {"n1", "n2"},
		},
		ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
			"c1/pg-preemptor": {
				api.Pipelined: 2,
			},
		},
	}

	trueValue := true
	// gang is enabled only for pipelined/starving of the preemptor gang, not for
	// Preemptable, so it does not veto evicting the victim gang.
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               capacity.PluginName,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	test.RegisterSession(tiers, []conf.Configuration{{Name: New().Name(),
		Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: false}}})
	defer test.Close()
	test.Run([]framework.Action{New()})
	if err := test.CheckAll(0); err != nil {
		t.Fatal(err)
	}
}

// TestPreemptLauncherOnScalarQueueViaNormalPreempt is the unit analog of the
// preempt-mpi-on-mpi e2e. On a queue that meters only the MIG scalar, a
// high-priority MPI gang (a MIG worker plus a CPU/memory-only launcher) preempts
// a same-spec low-priority gang. The launcher requests nothing the queue meters,
// so ssn.Allocatable is true for it and normalPreempt pipelines it without an
// idle fast path. This guards the decision to drop pipelineWithIdleResources from
// preempt: if capacity's CPU/memory defaulting or the Allocatable gate regresses,
// the launcher would no longer be placed and this test fails.
func TestPreemptLauncherOnScalarQueueViaNormalPreempt(t *testing.T) {
	const migResource = "nvidia.com/mig-3g.40gb"

	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	launcherLow := util.BuildPod("c1", "launcher-low", "n1", v1.PodRunning,
		api.BuildResourceList("1", "1Gi"),
		"pg-low", map[string]string{"app": "mpi"}, make(map[string]string))
	workerLow := util.BuildPod("c1", "worker-low", "n1", v1.PodRunning,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-low", map[string]string{"app": "mpi"}, make(map[string]string))
	for _, pod := range []*v1.Pod{launcherLow, workerLow} {
		pod.Annotations[schedulingv1beta1.PodPreemptable] = "true"
		pod.Annotations[framework.GroupEvictionPolicyAnnotationKey] = "minMember"
	}

	launcherHigh := util.BuildPod("c1", "launcher-high", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi"),
		"pg-high", map[string]string{"app": "mpi"}, make(map[string]string))
	workerHigh := util.BuildPod("c1", "worker-high", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
		"pg-high", map[string]string{"app": "mpi"}, make(map[string]string))

	test := &uthelper.TestCommonStruct{
		Name: "preempt places CPU/memory launcher on scalar-only queue via normalPreempt",
		Plugins: map[string]framework.PluginBuilder{
			capacity.PluginName:    capacity.New,
			conformance.PluginName: conformance.New,
			gang.PluginName:        gang.New,
			priority.PluginName:    priority.New,
			predicates.PluginName:  predicates.New,
		},
		PriClass: []*schedulingv1.PriorityClass{highPrio, lowPrio},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithPrio("pg-low", "c1", "q1", 2, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
			util.BuildPodGroupWithPrio("pg-high", "c1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
		},
		Pods: []*v1.Pod{launcherLow, workerLow, launcherHigh, workerHigh},
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: migResource, Value: "1"}, {Name: "pods", Value: "20"}}...), map[string]string{v1.LabelHostname: "n1"}),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueueWithResourcesQuantity("q1",
				api.BuildResourceList("", "", []api.ScalarResource{{Name: migResource, Value: "1"}}...),
				api.BuildResourceList("", "", []api.ScalarResource{{Name: migResource, Value: "1"}}...)),
		},
		ExpectEvicted:  []string{"c1/worker-low", "c1/launcher-low"},
		ExpectEvictNum: 2,
		ExpectPipeLined: map[string][]string{
			"c1/pg-high": {"n1"},
		},
		ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
			"c1/pg-high": {
				api.Pipelined: 2,
			},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               capacity.PluginName,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	test.RegisterSession(tiers, []conf.Configuration{{Name: New().Name(),
		Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: false}}})
	defer test.Close()
	test.Run([]framework.Action{New()})
	if err := test.CheckAll(0); err != nil {
		t.Fatal(err)
	}
}

func buildPodWithPodAntiAffinity(name, namespace, node string, phase v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string, topologyKey string) *v1.Pod {
	pod := util.BuildPod(name, namespace, node, phase, req, groupName, labels, selector)

	pod.Spec.Affinity = &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: labels,
					},
					TopologyKey: topologyKey,
				},
			},
		},
	}

	return pod
}
