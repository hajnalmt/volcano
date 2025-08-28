/*
Copyright 2021 The Volcano Authors.

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

package mate

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var (
	err error
	ctx *e2eutil.TestContext
)

var cmc *e2eutil.ConfigMapCase

var _ = BeforeSuite(func() {
	cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
	modifier := func(sc *e2eutil.SchedulerConfiguration) bool {
		trueValue := true
		for _, tier := range sc.Tiers {
			for i, plugin := range tier.Plugins {
				if plugin.Name == "proportion" {
					tier.Plugins[i] = e2eutil.PluginOption{
						Name:             "capacity",
						EnabledHierarchy: &trueValue,
					}
					return true
				}
			}
		}
		return false
	}
	cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
		return e2eutil.ModifySchedulerConfig(data, modifier)
	})
})

var _ = AfterSuite(func() {
	By("Cleaning up test resources")

	if cmc != nil {
		cmc.UndoChanged()
	}
})

// Define queue names
const (
	rootQueue = "root"
	parentA   = "parent-a"
	parentB   = "parent-b"
	q1        = "q1"
	q2        = "q2"
	q3        = "q3"
	q4        = "q4"
)

var _ = Describe("Mate E2E Test", func() {
	It("Hierarchy test: create root, 2 parents, 4 leaf queues", func() {
		By("Enable hierarchy and create hierarchical queue structure")

		// Creating a hierarchical queue structure like
		// 		root
		// 		/    \
		// parentA   parentB
		// /    \    /    \
		// q1    q2  q3    q4
		// parentA := &schedulingv1beta1.Queue{
		// 	ObjectMeta: metav1.ObjectMeta{Name: "parent-a"},
		// 	Spec: schedulingv1beta1.QueueSpec{
		// 		Parent: "root",
		// 	},
		// }
		// parentB := &schedulingv1beta1.Queue{
		// 	ObjectMeta: metav1.ObjectMeta{Name: "parent-b"},
		// 	Spec: schedulingv1beta1.QueueSpec{
		// 		Parent: "root",
		// 	},
		// }
		// leafQueues := []*schedulingv1beta1.Queue{
		// 	{
		// 		ObjectMeta: metav1.ObjectMeta{Name: "q1"},
		// 		Spec: schedulingv1beta1.QueueSpec{
		// 			Parent:     "parent-a",
		// 			Deserved:   v1.ResourceList{"nvidia.com/A100": resource.MustParse("2")},
		// 			Capability: v1.ResourceList{"nvidia.com/A100": resource.MustParse("3")},
		// 		},
		// 	},
		// 	{
		// 		ObjectMeta: metav1.ObjectMeta{Name: "q2"},
		// 		Spec: schedulingv1beta1.QueueSpec{
		// 			Parent:     "parent-a",
		// 			Deserved:   v1.ResourceList{"nvidia.com/A100": resource.MustParse("2")},
		// 			Capability: v1.ResourceList{"nvidia.com/A100": resource.MustParse("3")},
		// 		},
		// 	},
		// 	{
		// 		ObjectMeta: metav1.ObjectMeta{Name: "q3"},
		// 		Spec: schedulingv1beta1.QueueSpec{
		// 			Parent:     "parent-b",
		// 			Deserved:   v1.ResourceList{"nvidia.com/A100": resource.MustParse("2")},
		// 			Capability: v1.ResourceList{"nvidia.com/A100": resource.MustParse("3")},
		// 		},
		// 	},
		// 	{
		// 		ObjectMeta: metav1.ObjectMeta{Name: "q4"},
		// 		Spec: schedulingv1beta1.QueueSpec{
		// 			Parent:     "parent-b",
		// 			Deserved:   v1.ResourceList{"nvidia.com/A100": resource.MustParse("2")},
		// 			Capability: v1.ResourceList{"nvidia.com/A100": resource.MustParse("3")},
		// 		},
		// 	},
		// }

		// ctx := e2eutil.InitTestContext(e2eutil.Options{
		// 	Queues:        []string{"parent-a", "parent-b", "q1", "q2", "q3", "q4"},
		// 	NodesNumLimit: 4,
		// 	DeservedResource: map[string]v1.ResourceList{
		// 		q1: {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")},
		// 		q2: {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")},
		// 		q3: {v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")},
		// 		q4: {v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")},
		// 	},
		// 	QueueParent: map[string]string{
		// 		q1: "parent-a",
		// 		q2: "parent-a",
		// 		q3: "parent-b",
		// 		q4: "parent-b",
		// 	},
		// })

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{"parent-a", "q1", "q2"},
			NodesNumLimit: 4,
			DeservedResource: map[string]v1.ResourceList{
				q1: {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")},
				q2: {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")},
			},
			QueueParent: map[string]string{
				q1: "parent-a",
				q2: "parent-a",
			},
		})
		// defer e2eutil.CleanupTestContext(ctx)
		e2eutil.CreateSampleK8sJobWithKwokToleration(ctx, "q1", "nginx:latest", v1.ResourceList{v1.ResourceCPU: *resource.NewQuantity(1, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(1*1024*1024*1024, resource.BinarySI), "nvidia.com/A100": resource.MustParse("2")}, "q1")
		Expect(err).NotTo(HaveOccurred())

		// _, err = ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), parentA, metav1.CreateOptions{})
		// Expect(err).NotTo(HaveOccurred(), "failed to create parentA queue")
		// _, err = ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), parentB, metav1.CreateOptions{})
		// Expect(err).NotTo(HaveOccurred(), "failed to create parentB queue")
		// for _, q := range leafQueues {
		// 	_, err = ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), q, metav1.CreateOptions{})
		// 	Expect(err).NotTo(HaveOccurred(), "failed to create leaf queue %s", q.Name)
		// }

		By("Hierarchy queue structure created: root -> parentA/B -> q1/q2/q3/q4")
	})
})
