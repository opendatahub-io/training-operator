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
// limitations under the License.

package v1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestValidateV1XGBoostJob(t *testing.T) {
	validXGBoostReplicaSpecs := map[ReplicaType]*ReplicaSpec{
		XGBoostJobReplicaTypeMaster: {
			Replicas:      ptr.To[int32](1),
			RestartPolicy: RestartPolicyNever,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "xgboost",
						Image: "docker.io/kubeflow/xgboost-dist-iris:latest",
						Ports: []corev1.ContainerPort{{
							Name:          "xgboostjob-port",
							ContainerPort: 9991,
						}},
						ImagePullPolicy: corev1.PullAlways,
						Args: []string{
							"--job_type=Train",
							"--xgboost_parameter=objective:multi:softprob,num_class:3",
							"--n_estimators=10",
							"--learning_rate=0.1",
							"--model_path=/tmp/xgboost-model",
							"--model_storage_type=local",
						},
					}},
				},
			},
		},
		XGBoostJobReplicaTypeWorker: {
			Replicas:      ptr.To[int32](2),
			RestartPolicy: RestartPolicyExitCode,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "xgboost",
						Image: "docker.io/kubeflow/xgboost-dist-iris:latest",
						Ports: []corev1.ContainerPort{{
							Name:          "xgboostjob-port",
							ContainerPort: 9991,
						}},
						ImagePullPolicy: corev1.PullAlways,
						Args: []string{
							"--job_type=Train",
							"--xgboost_parameter=objective:multi:softprob,num_class:3",
							"--n_estimators=10",
							"--learning_rate=0.1",
						},
					}},
				},
			},
		},
	}

	testCases := map[string]struct {
		xgboostJob *XGBoostJob
		wantErr    bool
	}{
		"valid XGBoostJob": {
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: validXGBoostReplicaSpecs,
				},
			},
			wantErr: false,
		},
		"XGBoostJob name does not meet DNS1035": {
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "-test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: validXGBoostReplicaSpecs,
				},
			},
			wantErr: true,
		},
		"empty containers": {
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						XGBoostJobReplicaTypeWorker: {
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
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						XGBoostJobReplicaTypeWorker: {
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "xgboost",
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
		"xgboostJob default container name doesn't present": {
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						XGBoostJobReplicaTypeWorker: {
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "",
										Image: "gcr.io/kubeflow-ci/xgboost-dist-mnist_test:1.0",
									}},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"the number of replicas in masterReplica is other than 1": {
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						XGBoostJobReplicaTypeMaster: {
							Replicas: ptr.To[int32](2),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "xgboost",
										Image: "gcr.io/kubeflow-ci/xgboost-dist-mnist_test:1.0",
									}},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"masterReplica does not exist": {
			xgboostJob: &XGBoostJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: XGBoostJobSpec{
					XGBReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						XGBoostJobReplicaTypeWorker: {
							Replicas: ptr.To[int32](1),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "xgboost",
										Image: "gcr.io/kubeflow-ci/xgboost-dist-mnist_test:1.0",
									}},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := ValidateV1XGBoostJob(tc.xgboostJob)
			if (got != nil) != tc.wantErr {
				t.Fatalf("ValidateV1XGBoostJob() error = %v, wantErr %v", got, tc.wantErr)
			}
		})
	}
}
