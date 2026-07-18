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

// AccumulateCreditsFromVictim records the resources freed by a victim into the
// attempt-level credit maps. When the victim carries the group-eviction-policy
// "minMember" annotation, its whole PodGroup is expanded and every releasing
// sibling (including tasks on other nodes freed by the cascade) is credited.
// UIDs already present in committedEvictions or attemptEvictions are skipped to
// prevent double counting.
func AccumulateCreditsFromVictim(
	ssn *Session,
	victim *api.TaskInfo,
	attemptByNode map[string]*api.Resource,
	attemptTotal *api.Resource,
	attemptEvictions map[api.TaskID]struct{},
	committedEvictions map[api.TaskID]struct{},
) {
	candidates := []*api.TaskInfo{victim}
	if policy, ok := victim.Pod.Annotations[GroupEvictionPolicyAnnotationKey]; ok && policy == "minMember" {
		if job, found := ssn.Jobs[victim.Job]; found {
			for _, groupTask := range job.Tasks {
				if groupTask.UID != victim.UID {
					candidates = append(candidates, groupTask)
				}
			}
		}
	}

	for _, candidate := range candidates {
		if _, found := committedEvictions[candidate.UID]; found {
			continue
		}
		if _, found := attemptEvictions[candidate.UID]; found {
			continue
		}
		if candidate.Status != api.Releasing {
			continue
		}
		if len(candidate.NodeName) == 0 {
			continue
		}

		if _, found := attemptByNode[candidate.NodeName]; !found {
			attemptByNode[candidate.NodeName] = api.EmptyResource()
		}
		attemptByNode[candidate.NodeName].Add(candidate.Resreq)
		attemptTotal.Add(candidate.Resreq)
		attemptEvictions[candidate.UID] = struct{}{}
	}
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

// CloneCreditedSet returns a copy of an eviction UID set.
func CloneCreditedSet(evictions map[api.TaskID]struct{}) map[api.TaskID]struct{} {
	clone := make(map[api.TaskID]struct{}, len(evictions))
	for uid := range evictions {
		clone[uid] = struct{}{}
	}
	return clone
}
