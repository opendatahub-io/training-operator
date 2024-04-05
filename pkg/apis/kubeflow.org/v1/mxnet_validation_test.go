// Copyright 2021 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License

package v1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestValidateV1MXJob(t *testing.T) {
	validMXReplicaSpecs := map[ReplicaType]*ReplicaSpec{
		MXJobReplicaTypeScheduler: {
			Replicas:      ptr.To[int32](1),
			RestartPolicy: RestartPolicyNever,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "mxnet",
						Image: "mxjob/mxnet",
					}},
				},
			},
		},
		MXJobReplicaTypeServer: {
			Replicas:      ptr.To[int32](1),
			RestartPolicy: RestartPolicyNever,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "mxnet",
						Image: "mxjob/mxnet",
					}},
				},
			},
		},
		MXJobReplicaTypeWorker: {
			Replicas:      ptr.To[int32](1),
			RestartPolicy: RestartPolicyNever,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "mxnet",
						Image:   "mxjob/mxnet",
						Command: []string{"python"},
						Args: []string{
							"/incubator-mxnet/example/image-classification/train_mnist.py",
							"--num-epochs=10",
							"--num-layers=2",
							"--kv-store=dist_device_sync",
						},
					}},
				},
			},
		},
	}

	testCases := map[string]struct {
		MXJob   *MXJob
		wantErr bool
	}{
		"valid mxJob": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: MXJobSpec{
					MXReplicaSpecs: validMXReplicaSpecs,
				},
			},
			wantErr: false,
		},
		"mxReplicaSpecs is nil": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			wantErr: true,
		},
		"mxJob name does not meet DNS1035": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "10test",
				},
				Spec: MXJobSpec{
					MXReplicaSpecs: validMXReplicaSpecs,
				},
			},
			wantErr: true,
		},
		"no containers": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: MXJobSpec{
					MXReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						MXJobReplicaTypeWorker: {
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"image is empty": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: MXJobSpec{
					MXReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						MXJobReplicaTypeWorker: {
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "mxnet",
										Image: "",
									}},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"mxnet default container name doesn't find": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: MXJobSpec{
					MXReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						MXJobReplicaTypeWorker: {
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "",
										Image: "mxjob/mxnet:gpu",
									}},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"replicaSpec is nil": {
			MXJob: &MXJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: MXJobSpec{
					MXReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						MXJobReplicaTypeScheduler: nil,
					},
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := ValidateV1MXJob(tc.MXJob)
			if (got != nil) != tc.wantErr {
				t.Fatalf("ValidateV1MXJob() error = %v, wantErr %v", got, tc.wantErr)
			}
		})
	}
}
