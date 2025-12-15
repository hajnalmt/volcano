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

	// Create tasks (pods) in the job
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

	// Act: Evict task1
	err := stmt.Evict(task1, "test-reason")
	require.NoError(t, err)

	// Assert: All tasks with the annotation are scheduled for eviction
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

	// Act: Evict task2
	err = stmt.Evict(task2, "test2-reason")
	require.NoError(t, err)

	// Assert: All tasks with the annotation are scheduled for eviction
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
