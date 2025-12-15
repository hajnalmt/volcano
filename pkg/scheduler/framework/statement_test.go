/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added comprehensive test coverage for enhanced argument parsing functions

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

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type EvictedTask struct {
	UID    api.TaskID
	Reason string
}

func containsEvictedTask(evicted []EvictedTask, uid api.TaskID) bool {
	for _, task := range evicted {
		if task.UID == uid {
			return true
		}
	}

	return false
}

func TestStatementMerge(t *testing.T) {
	makeTask := func(name string) *api.TaskInfo {
		return &api.TaskInfo{
			Name: name,
		}
	}

	t.Run("merge transfers operations from source to target", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
				{name: Pipeline, task: makeTask("t2")},
			},
		}

		target.Merge(source)

		if len(target.operations) != 2 {
			t.Fatalf("expected 2 operations in target, got %d", len(target.operations))
		}

		if target.operations[0].task.Name != "t1" || target.operations[0].name != Evict {
			t.Errorf("first operation mismatch: got %v", target.operations[0])
		}

		if target.operations[1].task.Name != "t2" || target.operations[1].name != Pipeline {
			t.Errorf("second operation mismatch: got %v", target.operations[1])
		}
	})

	t.Run("source operations cleared after merge", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}

		target.Merge(source)

		if source.operations != nil {
			t.Errorf("expected source operations to be nil after merge, got %v", source.operations)
		}
	})

	t.Run("merge multiple sources", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Allocate, task: makeTask("t0")},
			},
		}
		src1 := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}
		src2 := &Statement{
			operations: []operation{
				{name: Pipeline, task: makeTask("t2")},
				{name: Evict, task: makeTask("t3"), reason: "preempt"},
			},
		}

		target.Merge(src1, src2)

		if len(target.operations) != 4 {
			t.Fatalf("expected 4 operations in target, got %d", len(target.operations))
		}

		expectedNames := []string{"t0", "t1", "t2", "t3"}
		for i, name := range expectedNames {
			if target.operations[i].task.Name != name {
				t.Errorf("operation %d: expected task %s, got %s", i, name, target.operations[i].task.Name)
			}
		}

		if src1.operations != nil {
			t.Errorf("expected src1 operations to be nil after merge")
		}

		if src2.operations != nil {
			t.Errorf("expected src2 operations to be nil after merge")
		}
	})

	t.Run("merge empty source is no-op", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}
		empty := &Statement{}

		target.Merge(empty)

		if len(target.operations) != 1 {
			t.Errorf("expected 1 operation in target after merging empty, got %d", len(target.operations))
		}
	})

	t.Run("merge into empty target", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Pipeline, task: makeTask("t1")},
			},
		}

		target.Merge(source)

		if len(target.operations) != 1 {
			t.Fatalf("expected 1 operation in target, got %d", len(target.operations))
		}

		if target.operations[0].task.Name != "t1" {
			t.Errorf("expected task t1, got %s", target.operations[0].task.Name)
		}
	})

	t.Run("merge with no arguments is no-op", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}

		target.Merge()

		if len(target.operations) != 1 {
			t.Errorf("expected 1 operation after no-arg merge, got %d", len(target.operations))
		}
	})
}

func TestGroupEvictionPolicy(t *testing.T) {
	var logLevel klog.Level
	logLevel.Set("5")

	ssn := &Session{
		UID:           "test-session",
		Jobs:          map[api.JobID]*api.JobInfo{},
		Nodes:         map[string]*api.NodeInfo{},
		eventHandlers: nil,
	}
	job := &api.JobInfo{
		UID:             "job1",
		Name:            "job1",
		TotalRequest:    api.EmptyResource(),
		Allocated:       api.EmptyResource(),
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{},
		Tasks:           map[api.TaskID]*api.TaskInfo{},
		SubJobs:         map[api.SubJobID]*api.SubJobInfo{},
		TaskToSubJob:    map[api.TaskID]api.SubJobID{},
	}
	ssn.Jobs[job.UID] = job

	task1 := &api.TaskInfo{
		UID:    "1",
		Job:    job.UID,
		Resreq: api.EmptyResource(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		},
	}
	task2 := &api.TaskInfo{
		UID:    "2",
		Job:    job.UID,
		Resreq: api.EmptyResource(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{GroupEvictionPolicyAnnotationKey: "minMember"},
			},
		},
	}
	task3 := &api.TaskInfo{
		UID:    "3",
		Job:    job.UID,
		Resreq: api.EmptyResource(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{GroupEvictionPolicyAnnotationKey: "minMember"},
			},
		},
	}
	job.Tasks = map[api.TaskID]*api.TaskInfo{
		"1": task1,
		"2": task2,
		"3": task3,
	}

	stmt := NewStatement(ssn)

	err := stmt.Evict(task1, "test-reason")
	require.NoError(t, err)

	evicted := []EvictedTask{}
	for _, op := range stmt.operations {
		if op.name == Evict {
			evicted = append(evicted, EvictedTask{
				UID:    op.task.UID,
				Reason: op.reason,
			})
		}
	}

	require.True(t, containsEvictedTask(evicted, api.TaskID("1")), "task1 should be evicted")
	require.False(t, containsEvictedTask(evicted, api.TaskID("2")), "task2 should not be evicted")
	require.False(t, containsEvictedTask(evicted, api.TaskID("3")), "task3 should not be evicted")
	require.Equal(t, 1, len(evicted), "only task1 should be evicted")
	require.Equal(t, "test-reason", evicted[0].Reason, "task1 should have correct reason")

	err = stmt.Evict(task2, "test2-reason")
	require.NoError(t, err)

	evicted = []EvictedTask{}
	for _, op := range stmt.operations {
		if op.name == Evict {
			evicted = append(evicted, EvictedTask{
				UID:    op.task.UID,
				Reason: op.reason,
			})
		}
	}
	require.Equal(t, 3, len(evicted), "all three tasks should be evicted")
	require.Equal(t, "test-reason", evicted[0].Reason, "task1 should have correct reason")
	require.Equal(t, "test2-reason", evicted[1].Reason, "task2 should have correct reason")
	require.Equal(t, "group-eviction-policy", evicted[2].Reason, "test3 should have correct reason")
}
