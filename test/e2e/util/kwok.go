/*
Copyright 2025 The Volcano Authors.

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

package util

import (
	context "context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateKwokA100Node creates a KWOK node with A100 GPU resources
func CreateKwokA100Node(ctx *TestContext, nodeName string, cpu int, memory string, gpu int) error {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"beta.kubernetes.io/arch":       "amd64",
				"beta.kubernetes.io/os":         "linux",
				"kubernetes.io/arch":            "amd64",
				"kubernetes.io/hostname":        nodeName,
				"kubernetes.io/os":              "linux",
				"kubernetes.io/role":            "agent",
				"node-role.kubernetes.io/agent": "",
				"type":                          "kwok",
			},
			Annotations: map[string]string{
				"node.alpha.kubernetes.io/ttl": "0",
				"kwok.x-k8s.io/node":           "fake",
			},
		},
		Spec: v1.NodeSpec{
			Taints: []v1.Taint{{
				Key:    "kwok.x-k8s.io/node",
				Value:  "fake",
				Effect: v1.TaintEffectNoSchedule,
			}},
		},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalSI),
				v1.ResourceMemory: resource.MustParse(memory),
				v1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
				"nvidia.com/A100": *resource.NewQuantity(int64(gpu), resource.DecimalSI),
			},
			Allocatable: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalSI),
				v1.ResourceMemory: resource.MustParse(memory),
				v1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
				"nvidia.com/A100": *resource.NewQuantity(int64(gpu), resource.DecimalSI),
			},
		},
	}
	_, err := ctx.Kubeclient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	return err
}

// CreateKwokH100Node creates a KWOK node with H100 GPU resources
func CreateKwokH100Node(ctx *TestContext, nodeName string, cpu int, memory string, gpu int) error {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"beta.kubernetes.io/arch":       "amd64",
				"beta.kubernetes.io/os":         "linux",
				"kubernetes.io/arch":            "amd64",
				"kubernetes.io/hostname":        nodeName,
				"kubernetes.io/os":              "linux",
				"kubernetes.io/role":            "agent",
				"node-role.kubernetes.io/agent": "",
				"type":                          "kwok",
			},
			Annotations: map[string]string{
				"node.alpha.kubernetes.io/ttl": "0",
				"kwok.x-k8s.io/node":           "fake",
			},
		},
		Spec: v1.NodeSpec{
			Taints: []v1.Taint{{
				Key:    "kwok.x-k8s.io/node",
				Value:  "fake",
				Effect: v1.TaintEffectNoSchedule,
			}},
		},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalSI),
				v1.ResourceMemory: resource.MustParse(memory),
				v1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
				"nvidia.com/H100": *resource.NewQuantity(int64(gpu), resource.DecimalSI),
			},
			Allocatable: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalSI),
				v1.ResourceMemory: resource.MustParse(memory),
				v1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
				"nvidia.com/H100": *resource.NewQuantity(int64(gpu), resource.DecimalSI),
			},
		},
	}
	_, err := ctx.Kubeclient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	return err
}

func DeleteKwokNode(ctx *TestContext, nodeName string) error {
	return ctx.Kubeclient.CoreV1().Nodes().Delete(context.TODO(), nodeName, metav1.DeleteOptions{})
}
