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

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateSampleK8sJobWithTolerations creates a new k8s job with tolerations
func CreateSampleK8sJobWithTolerations(ctx *TestContext, name string, img string, req v1.ResourceList, queueName string, tolerations []v1.Toleration) *batchv1.Job {
	k8sjobname := "job.k8s.io"
	defaultTrue := true
	annotations := map[string]string{
		"scheduling.volcano.sh/queue-name":  queueName,
		"scheduling.volcano.sh/preemptable": "true",
	}
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: batchv1.JobSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					k8sjobname: name,
				},
			},
			ManualSelector: &defaultTrue,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{k8sjobname: name},
					Annotations: annotations,
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
					RestartPolicy: v1.RestartPolicyOnFailure,
					Tolerations:   tolerations,
					Containers: []v1.Container{
						{
							Image:           img,
							Name:            name,
							Command:         []string{"/bin/sh", "-c", "sleep 10"},
							ImagePullPolicy: v1.PullIfNotPresent,
							Resources: v1.ResourceRequirements{
								Limits:   req,
								Requests: req,
							},
						},
					},
				},
			},
		},
	}

	jb, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Create(context.TODO(), j, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create k8sjob %s", name)

	return jb
}

// CreateSampleK8sJobWithKwokToleration creates a new k8s job with a default KWOK node toleration
func CreateSampleK8sJobWithKwokToleration(ctx *TestContext, name string, img string, req v1.ResourceList, queueName string) *batchv1.Job {
	tolerations := []v1.Toleration{
		{
			Key:      "kwok.x-k8s.io/node",
			Operator: v1.TolerationOpEqual,
			Value:    "fake",
			Effect:   v1.TaintEffectNoSchedule,
		},
	}
	return CreateSampleK8sJobWithTolerations(ctx, name, img, req, queueName, tolerations)
}
