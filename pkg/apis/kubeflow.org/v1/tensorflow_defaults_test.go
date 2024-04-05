// Copyright 2018 The Kubeflow Authors
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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func expectedTFJob(cleanPodPolicy CleanPodPolicy, restartPolicy RestartPolicy, portName string, port int32) *TFJob {
	var ports []corev1.ContainerPort

	// port not set
	if portName != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          portName,
				ContainerPort: port,
			},
		)
	}

	// port set with custom name
	if portName != TFJobDefaultPortName {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          TFJobDefaultPortName,
				ContainerPort: TFJobDefaultPort,
			},
		)
	}

	defaultSuccessPolicy := SuccessPolicyDefault

	return &TFJob{
		Spec: TFJobSpec{
			SuccessPolicy: &defaultSuccessPolicy,
			RunPolicy: RunPolicy{
				CleanPodPolicy: &cleanPodPolicy,
			},
			TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
				TFJobReplicaTypeWorker: &ReplicaSpec{
					Replicas:      ptr.To[int32](1),
					RestartPolicy: restartPolicy,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								corev1.Container{
									Name:  TFJobDefaultContainerName,
									Image: testImage,
									Ports: ports,
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestSetTypeNames(t *testing.T) {
	spec := &ReplicaSpec{
		RestartPolicy: RestartPolicyAlways,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					corev1.Container{
						Name:  TFJobDefaultContainerName,
						Image: testImage,
						Ports: []corev1.ContainerPort{
							corev1.ContainerPort{
								Name:          TFJobDefaultPortName,
								ContainerPort: TFJobDefaultPort,
							},
						},
					},
				},
			},
		},
	}

	workerUpperCase := ReplicaType("WORKER")
	original := &TFJob{
		Spec: TFJobSpec{
			TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
				workerUpperCase: spec,
			},
		},
	}

	setTensorflowTypeNamesToCamelCase(original)
	if _, ok := original.Spec.TFReplicaSpecs[workerUpperCase]; ok {
		t.Errorf("Failed to delete key %s", workerUpperCase)
	}
	if _, ok := original.Spec.TFReplicaSpecs[TFJobReplicaTypeWorker]; !ok {
		t.Errorf("Failed to set key %s", TFJobReplicaTypeWorker)
	}
}

func TestSetDefaultTFJob(t *testing.T) {
	customPortName := "customPort"
	var customPort int32 = 1234
	customRestartPolicy := RestartPolicyAlways

	testCases := map[string]struct {
		original *TFJob
		expected *TFJob
	}{
		"set replicas": {
			original: &TFJob{
				Spec: TFJobSpec{
					TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						TFJobReplicaTypeWorker: &ReplicaSpec{
							RestartPolicy: customRestartPolicy,
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										corev1.Container{
											Name:  TFJobDefaultContainerName,
											Image: testImage,
											Ports: []corev1.ContainerPort{
												{
													Name:          TFJobDefaultPortName,
													ContainerPort: TFJobDefaultPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: expectedTFJob(CleanPodPolicyNone, customRestartPolicy, TFJobDefaultPortName, TFJobDefaultPort),
		},
		"set replicas with default restartpolicy": {
			original: &TFJob{
				Spec: TFJobSpec{
					TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						TFJobReplicaTypeWorker: &ReplicaSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										corev1.Container{
											Name:  TFJobDefaultContainerName,
											Image: testImage,
											Ports: []corev1.ContainerPort{
												corev1.ContainerPort{
													Name:          TFJobDefaultPortName,
													ContainerPort: TFJobDefaultPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: expectedTFJob(CleanPodPolicyNone, TFJobDefaultRestartPolicy, TFJobDefaultPortName, TFJobDefaultPort),
		},
		"set replicas with default port": {
			original: &TFJob{
				Spec: TFJobSpec{
					TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						TFJobReplicaTypeWorker: &ReplicaSpec{
							Replicas:      ptr.To[int32](1),
							RestartPolicy: customRestartPolicy,
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										corev1.Container{
											Name:  TFJobDefaultContainerName,
											Image: testImage,
										},
									},
								},
							},
						},
					},
				},
			},
			expected: expectedTFJob(CleanPodPolicyNone, customRestartPolicy, "", 0),
		},
		"set replicas adding default port": {
			original: &TFJob{
				Spec: TFJobSpec{
					TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						TFJobReplicaTypeWorker: &ReplicaSpec{
							Replicas:      ptr.To[int32](1),
							RestartPolicy: customRestartPolicy,
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										corev1.Container{
											Name:  TFJobDefaultContainerName,
											Image: testImage,
											Ports: []corev1.ContainerPort{
												corev1.ContainerPort{
													Name:          customPortName,
													ContainerPort: customPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: expectedTFJob(CleanPodPolicyNone, customRestartPolicy, customPortName, customPort),
		},
		"set custom cleanpod policy": {
			original: &TFJob{
				Spec: TFJobSpec{
					RunPolicy: RunPolicy{
						CleanPodPolicy: CleanPodPolicyPointer(CleanPodPolicyAll),
					},
					TFReplicaSpecs: map[ReplicaType]*ReplicaSpec{
						TFJobReplicaTypeWorker: &ReplicaSpec{
							Replicas:      ptr.To[int32](1),
							RestartPolicy: customRestartPolicy,
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										corev1.Container{
											Name:  TFJobDefaultContainerName,
											Image: testImage,
											Ports: []corev1.ContainerPort{
												corev1.ContainerPort{
													Name:          customPortName,
													ContainerPort: customPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: expectedTFJob(CleanPodPolicyAll, customRestartPolicy, customPortName, customPort),
		},
	}

	for name, tc := range testCases {
		SetDefaults_TFJob(tc.original)
		if !reflect.DeepEqual(tc.original, tc.expected) {
			t.Errorf("%s: Want\n%v; Got\n %v", name, tc.expected, tc.original)
		}
	}
}
