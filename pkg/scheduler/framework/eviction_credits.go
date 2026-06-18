/*
Copyright 2018-2025 The Volcano Authors.

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
	"sort"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// AccumulateCreditsFromVictim records the resources freed by victim into the
// attempt-level credit maps, skipping UIDs already in committedEvictions or
// attemptEvictions to prevent double-counting.
func AccumulateCreditsFromVictim(
	victim *api.TaskInfo,
	attemptByNode map[string]*api.Resource,
	attemptTotal *api.Resource,
	attemptEvictions map[api.TaskID]struct{},
	committedEvictions map[api.TaskID]struct{},
) {
	if victim.Status != api.Releasing {
		return
	}
	if len(victim.NodeName) == 0 {
		return
	}
	if _, found := committedEvictions[victim.UID]; found {
		return
	}
	if _, found := attemptEvictions[victim.UID]; found {
		return
	}

	if _, found := attemptByNode[victim.NodeName]; !found {
		attemptByNode[victim.NodeName] = api.EmptyResource()
	}
	attemptByNode[victim.NodeName].Add(victim.Resreq)
	attemptTotal.Add(victim.Resreq)
	attemptEvictions[victim.UID] = struct{}{}
}

// MergeCredits merges attempt-level credit maps into the committed session-level maps.
func MergeCredits(
	byNode map[string]*api.Resource,
	total *api.Resource,
	evictions map[api.TaskID]struct{},
	attemptByNode map[string]*api.Resource,
	attemptTotal *api.Resource,
	attemptEvictions map[api.TaskID]struct{},
) {
	for nodeName, res := range attemptByNode {
		if _, found := byNode[nodeName]; !found {
			byNode[nodeName] = api.EmptyResource()
		}
		byNode[nodeName].Add(res)
	}
	total.Add(attemptTotal)
	for uid := range attemptEvictions {
		evictions[uid] = struct{}{}
	}
}

// HasSufficientCredits reports whether both the per-node and total credits can
// cover task's full request.
func HasSufficientCredits(
	task *api.TaskInfo,
	nodeName string,
	byNode map[string]*api.Resource,
	total *api.Resource,
) bool {
	if !task.InitResreq.LessEqual(total, api.Zero) {
		return false
	}
	nodeCredits, found := byNode[nodeName]
	if !found {
		return false
	}
	return task.InitResreq.LessEqual(nodeCredits, api.Zero)
}

// ConsumeCredits subtracts task's request from both the per-node and total
// credit pools. No-op if credits are insufficient (call HasSufficientCredits first).
func ConsumeCredits(
	task *api.TaskInfo,
	nodeName string,
	byNode map[string]*api.Resource,
	total *api.Resource,
) {
	nodeCredits, found := byNode[nodeName]
	if !found {
		return
	}
	nodeCredits.Sub(task.InitResreq)
	total.Sub(task.InitResreq)
}

// CreditNodeNames returns the names of nodes that have non-empty credits,
// sorted for deterministic iteration.
func CreditNodeNames(byNode map[string]*api.Resource) []string {
	names := make([]string, 0, len(byNode))
	for name, credits := range byNode {
		if credits != nil && !credits.IsEmpty() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// CloneCreditsByNode returns a deep copy of a per-node credit map.
func CloneCreditsByNode(byNode map[string]*api.Resource) map[string]*api.Resource {
	clone := make(map[string]*api.Resource, len(byNode))
	for name, credits := range byNode {
		if credits == nil {
			clone[name] = nil
			continue
		}
		clone[name] = credits.Clone()
	}
	return clone
}

// CloneCreditedSet returns a shallow copy of an eviction UID set.
func CloneCreditedSet(evictions map[api.TaskID]struct{}) map[api.TaskID]struct{} {
	clone := make(map[api.TaskID]struct{}, len(evictions))
	for uid := range evictions {
		clone[uid] = struct{}{}
	}
	return clone
}
