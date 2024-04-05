// Copyright 2021 The Kubeflow Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mpi

import (
	"context"
	"fmt"
	"strings"

	common "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeflowv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	commonutil "github.com/kubeflow/training-operator/pkg/util"
	"github.com/kubeflow/training-operator/pkg/util/testutil"
)

const (
	gpuResourceName         = "nvidia.com/gpu"
	extendedGPUResourceName = "vendor-domain/gpu"
)

func newMPIJobCommon(name string, startTime, completionTime *metav1.Time) *kubeflowv1.MPIJob {
	mpiJob := &kubeflowv1.MPIJob{
		TypeMeta: metav1.TypeMeta{APIVersion: kubeflowv1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: kubeflowv1.MPIJobSpec{
			RunPolicy: common.RunPolicy{
				CleanPodPolicy: kubeflowv1.CleanPodPolicyPointer(kubeflowv1.CleanPodPolicyAll),
			},
			MPIReplicaSpecs: map[common.ReplicaType]*common.ReplicaSpec{
				kubeflowv1.MPIJobReplicaTypeWorker: {
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "bar",
								},
							},
						},
					},
				},
				kubeflowv1.MPIJobReplicaTypeLauncher: {
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "bar",
								},
							},
						},
					},
				},
			},
		},
		Status: common.JobStatus{},
	}

	if startTime != nil {
		mpiJob.Status.StartTime = startTime
	}
	if completionTime != nil {
		mpiJob.Status.CompletionTime = completionTime
	}

	return mpiJob
}

func newMPIJobOld(name string, replicas *int32, pusPerReplica int64, resourceName string, startTime, completionTime *metav1.Time) *kubeflowv1.MPIJob {
	mpiJob := newMPIJobCommon(name, startTime, completionTime)

	mpiJob.Spec.MPIReplicaSpecs[kubeflowv1.MPIJobReplicaTypeWorker].Replicas = replicas

	workerContainers := mpiJob.Spec.MPIReplicaSpecs[kubeflowv1.MPIJobReplicaTypeWorker].Template.Spec.Containers
	for i := range workerContainers {
		container := &workerContainers[i]
		container.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceName(resourceName): *resource.NewQuantity(pusPerReplica, resource.DecimalExponent),
			},
		}
	}

	return mpiJob
}

var newMPIJob = newMPIJobWithLauncher

func newMPIJobWithLauncher(name string, replicas *int32, pusPerReplica int64, resourceName string, startTime, completionTime *metav1.Time) *kubeflowv1.MPIJob {
	mpiJob := newMPIJobOld(name, replicas, pusPerReplica, resourceName, startTime, completionTime)

	mpiJob.Spec.MPIReplicaSpecs[kubeflowv1.MPIJobReplicaTypeLauncher].Replicas = ptr.To[int32](1)

	launcherContainers := mpiJob.Spec.MPIReplicaSpecs[kubeflowv1.MPIJobReplicaTypeLauncher].Template.Spec.Containers
	for i := range launcherContainers {
		container := &launcherContainers[i]
		container.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceName(resourceName): *resource.NewQuantity(pusPerReplica, resource.DecimalExponent),
			},
		}
	}

	return mpiJob
}

var _ = Describe("MPIJob controller", func() {
	Context("Test launcher is GPU launcher", func() {
		It("Should pass GPU Launcher verification", func() {
			By("By creating MPIJobs with various resource configuration")

			testCases := map[string]struct {
				gpu      string
				expected bool
			}{
				"isNvidiaGPU": {
					gpu:      gpuResourceName,
					expected: true,
				},
				"isExtendedGPU": {
					gpu:      extendedGPUResourceName,
					expected: true,
				},
				"notGPU": {
					gpu:      "vendor-domain/resourcetype",
					expected: false,
				},
			}

			startTime := metav1.Now()
			completionTime := metav1.Now()

			for testName, testCase := range testCases {
				mpiJob := newMPIJobWithLauncher("test-"+strings.ToLower(testName),
					ptr.To[int32](64), 1, testCase.gpu, &startTime, &completionTime)
				Expect(isGPULauncher(mpiJob) == testCase.expected).To(BeTrue())
			}
		})
	})

	Context("Test MPIJob with succeeded launcher Pod", func() {
		It("Should contains desired launcher ReplicaStatus", func() {
			By("By marking a launcher pod with Phase Succeeded")
			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			jobName := "test-launcher-succeeded"

			mpiJob := newMPIJobWithLauncher(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", isGPULauncher(mpiJob))
			launcher.Status.Phase = corev1.PodSucceeded

			launcherKey := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      launcher.GetName(),
			}
			Eventually(func() error {
				launcherCreated := &corev1.Pod{}
				if err := testK8sClient.Get(ctx, launcherKey, launcherCreated); err != nil {
					return err
				}
				launcherCreated.Status.Phase = corev1.PodSucceeded
				return testK8sClient.Status().Update(ctx, launcherCreated)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			created := &kubeflowv1.MPIJob{}
			launcherStatus := &common.ReplicaStatus{
				Active:    0,
				Succeeded: 1,
				Failed:    0,
			}
			Eventually(func() bool {
				err := testK8sClient.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: jobName}, created)
				if err != nil {
					return false
				}
				return ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeLauncher, launcherStatus)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
		})
	})

	Context("Test MPIJob with failed launcher Pod", func() {
		It("Should contains desired launcher ReplicaStatus", func() {
			By("By marking a launcher pod with Phase Failed")
			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			jobName := "test-launcher-failed"

			mpiJob := newMPIJobWithLauncher(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", isGPULauncher(mpiJob))
			launcherKey := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      launcher.GetName(),
			}
			Eventually(func() error {
				launcherCreated := &corev1.Pod{}
				if err := testK8sClient.Get(ctx, launcherKey, launcherCreated); err != nil {
					return err
				}
				launcherCreated.Status.Phase = corev1.PodFailed
				return testK8sClient.Status().Update(ctx, launcherCreated)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			launcherStatus := &common.ReplicaStatus{
				Active:    0,
				Succeeded: 0,
				Failed:    1,
			}
			created := &kubeflowv1.MPIJob{}
			Eventually(func() bool {
				err := testK8sClient.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: jobName}, created)
				if err != nil {
					return false
				}
				return ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeLauncher, launcherStatus)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
		})
	})

	Context("Test MPIJob with succeeded launcher pod", func() {
		It("Should contain desired ReplicaStatuses for worker", func() {
			By("By marking the launcher Pod as Succeeded")
			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			jobName := "test-launcher-succeeded2"

			mpiJob := newMPIJobWithLauncher(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", isGPULauncher(mpiJob))
			launcher.Status.Phase = corev1.PodSucceeded

			launcherKey := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      launcher.GetName(),
			}
			Eventually(func() error {
				launcherCreated := &corev1.Pod{}
				if err := testK8sClient.Get(ctx, launcherKey, launcherCreated); err != nil {
					return err
				}
				launcherCreated.Status.Phase = corev1.PodSucceeded
				return testK8sClient.Status().Update(ctx, launcherCreated)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			created := &kubeflowv1.MPIJob{}
			launcherStatus := &common.ReplicaStatus{
				Active:    0,
				Succeeded: 0,
				Failed:    0,
			}
			Eventually(func() bool {
				err := testK8sClient.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: jobName}, created)
				if err != nil {
					return false
				}
				return ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeWorker, launcherStatus)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
		})
	})

	Context("Test MPIJob with Running launcher Pod and Pending worker Pods", func() {
		It("Should contain desired ReplicaStatuses", func() {
			By("By marking an active launcher pod and pending worker pods")

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			jobName := "test-launcher-running-worker-pending"

			var replicas int32 = 8
			mpiJob := newMPIJobWithLauncher(jobName, &replicas, 1, gpuResourceName, &startTime, &completionTime)
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", isGPULauncher(mpiJob))
			launcherKey := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      launcher.GetName(),
			}
			Eventually(func() error {
				launcherCreated := &corev1.Pod{}
				if err := testK8sClient.Get(ctx, launcherKey, launcherCreated); err != nil {
					return err
				}
				launcherCreated.Status.Phase = corev1.PodRunning
				return testK8sClient.Status().Update(ctx, launcherCreated)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			for i := 0; i < int(replicas); i++ {
				name := fmt.Sprintf("%s-%d", mpiJob.Name+workerSuffix, i)
				worker := reconciler.newWorker(mpiJob, name)
				workerKey := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      worker.GetName(),
				}
				Eventually(func() error {
					workerCreated := &corev1.Pod{}
					if err := testK8sClient.Get(ctx, workerKey, workerCreated); err != nil {
						return err
					}
					workerCreated.Status.Phase = corev1.PodPending
					return testK8sClient.Status().Update(ctx, workerCreated)
				}, testutil.Timeout, testutil.Interval).Should(BeNil())
			}

			key := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      jobName,
			}
			launcherStatus := &common.ReplicaStatus{
				Active:    1,
				Succeeded: 0,
				Failed:    0,
			}
			workerStatus := &common.ReplicaStatus{
				Active:    0,
				Succeeded: 0,
				Failed:    0,
			}
			Eventually(func() bool {
				created := &kubeflowv1.MPIJob{}
				err := testK8sClient.Get(ctx, key, created)
				if err != nil {
					return false
				}
				return ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeLauncher,
					launcherStatus) && ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeWorker,
					workerStatus)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
		})
	})

	Context("Test MPIJob with Running launcher Pod and Running worker Pods", func() {
		It("Should contain desired ReplicaStatuses", func() {
			By("By creating an active launcher pod and active worker pods")

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			jobName := "test-launcher-running-worker-running"

			var replicas int32 = 8
			mpiJob := newMPIJob(jobName, &replicas, 1, gpuResourceName, &startTime, &completionTime)
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", isGPULauncher(mpiJob))
			launcherKey := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      launcher.GetName(),
			}
			Eventually(func() error {
				launcherCreated := &corev1.Pod{}
				if err := testK8sClient.Get(ctx, launcherKey, launcherCreated); err != nil {
					return err
				}
				launcherCreated.Status.Phase = corev1.PodRunning
				return testK8sClient.Status().Update(ctx, launcherCreated)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			for i := 0; i < int(replicas); i++ {
				name := fmt.Sprintf("%s-%d", mpiJob.Name+workerSuffix, i)
				worker := reconciler.newWorker(mpiJob, name)
				workerKey := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      worker.GetName(),
				}
				Eventually(func() error {
					workerCreated := &corev1.Pod{}
					if err := testK8sClient.Get(ctx, workerKey, workerCreated); err != nil {
						return err
					}
					workerCreated.Status.Phase = corev1.PodRunning
					return testK8sClient.Status().Update(ctx, workerCreated)
				}, testutil.Timeout, testutil.Interval).Should(BeNil())
			}

			key := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      jobName,
			}
			launcherStatus := &common.ReplicaStatus{
				Active:    1,
				Succeeded: 0,
				Failed:    0,
			}
			workerStatus := &common.ReplicaStatus{
				Active:    8,
				Succeeded: 0,
				Failed:    0,
			}
			Eventually(func() bool {
				created := &kubeflowv1.MPIJob{}
				err := testK8sClient.Get(ctx, key, created)
				if err != nil {
					return false
				}
				return ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeLauncher,
					launcherStatus) && ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeWorker,
					workerStatus)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
		})
	})

	Context("Test MPIJob with Running worker Pods", func() {
		It("Should contain desired ReplicaStatuses and create a launcher pod", func() {
			By("By creating only active worker pods")

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			jobName := "test-worker-running"

			var replicas int32 = 16
			mpiJob := newMPIJob(jobName, &replicas, 1, gpuResourceName, &startTime, &completionTime)
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			for i := 0; i < int(replicas); i++ {
				name := fmt.Sprintf("%s-%d", mpiJob.Name+workerSuffix, i)
				worker := reconciler.newWorker(mpiJob, name)
				workerKey := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      worker.GetName(),
				}
				Eventually(func() error {
					workerCreated := &corev1.Pod{}
					if err := testK8sClient.Get(ctx, workerKey, workerCreated); err != nil {
						return err
					}
					workerCreated.Status.Phase = corev1.PodRunning
					return testK8sClient.Status().Update(ctx, workerCreated)
				}, testutil.Timeout, testutil.Interval).Should(BeNil())
			}

			launcherKey := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      mpiJob.Name + launcherSuffix,
			}
			launcher := &kubeflowv1.MPIJob{}
			Eventually(func() bool {
				err := testK8sClient.Get(ctx, launcherKey, launcher)
				return err != nil
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())

			key := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      jobName,
			}
			launcherStatus := &common.ReplicaStatus{
				Active:    0,
				Succeeded: 0,
				Failed:    0,
			}
			workerStatus := &common.ReplicaStatus{
				Active:    16,
				Succeeded: 0,
				Failed:    0,
			}
			Eventually(func() bool {
				created := &kubeflowv1.MPIJob{}
				err := testK8sClient.Get(ctx, key, created)
				if err != nil {
					return false
				}
				return ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeLauncher,
					launcherStatus) && ReplicaStatusMatch(created.Status.ReplicaStatuses, kubeflowv1.MPIJobReplicaTypeWorker,
					workerStatus)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
		})
	})

	Context("MPIJob not found", func() {
		It("Should do nothing", func() {
			By("Calling Reconcile method")
			jobName := "test-not-exist"

			ctx := context.Background()

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      jobName,
			}}
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).Should(BeNil())
		})
	})

	Context("MPI Job succeeds with predefined service account", func() {
		It("should run with the defined service account", func() {
			By("Calling Reconcile method")
			jobName := "test-sa-orphan"
			launcherSaName := "launcher-sa"

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			mpiJob := newMPIJob(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)
			mpiJob.Spec.MPIReplicaSpecs[kubeflowv1.MPIJobReplicaTypeLauncher].Template.Spec.ServiceAccountName = launcherSaName
			sa := newLauncherServiceAccount(mpiJob)
			sa.OwnerReferences = nil

			Expect(sa.Name).Should(Equal(launcherSaName))
			Expect(testK8sClient.Create(ctx, sa)).Should(Succeed())
			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			Eventually(func() error {
				req := ctrl.Request{NamespacedName: types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      mpiJob.GetName(),
				}}

				_, err := reconciler.Reconcile(ctx, req)

				if err != nil {
					return err
				}

				return nil
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			Eventually(func() string {
				launcherCreated := &corev1.Pod{}

				launcherKey := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      mpiJob.Name + launcherSuffix,
				}

				if err := testK8sClient.Get(ctx, launcherKey, launcherCreated); err != nil {
					return ""
				}

				return launcherCreated.Spec.ServiceAccountName
			}, testutil.Timeout, testutil.Interval).Should(Equal(launcherSaName))
		})
	})

	Context("MPIJob with launcher Pod not controlled by itself", func() {
		It("Should return error", func() {
			By("Calling Reconcile method")
			jobName := "test-launcher-orphan"
			testKind := "Pod"

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			mpiJob := newMPIJob(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)

			launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", isGPULauncher(mpiJob))
			launcher.OwnerReferences = nil
			Expect(testK8sClient.Create(ctx, launcher)).Should(Succeed())

			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      mpiJob.GetName(),
			}}
			expectedErr := fmt.Errorf(MessageResourceExists, launcher.Name, testKind)
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, testutil.Timeout, testutil.Interval).Should(MatchError(expectedErr))
		})
	})

	Context("MPIJob with worker Pod not controlled by itself", func() {
		It("Should return error", func() {
			By("Calling Reconcile method")
			jobName := "test-worker-orphan"
			testKind := "Pod"

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			mpiJob := newMPIJob(jobName, ptr.To[int32](1), 1, gpuResourceName, &startTime, &completionTime)

			for i := 0; i < 1; i++ {
				name := fmt.Sprintf("%s-%d", mpiJob.Name+workerSuffix, i)
				worker := reconciler.newWorker(mpiJob, name)
				worker.OwnerReferences = nil
				Expect(testK8sClient.Create(ctx, worker)).Should(Succeed())
			}

			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      mpiJob.GetName(),
			}}
			expectedErr := fmt.Errorf(MessageResourceExists, fmt.Sprintf("%s-%d", mpiJob.Name+workerSuffix, 0), testKind)
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, testutil.Timeout, testutil.Interval).Should(MatchError(expectedErr))
		})
	})

	Context("MPIJob with ConfigMap not controlled by itself", func() {
		It("Should return error", func() {
			By("Calling Reconcile method")
			jobName := "test-cm-orphan"
			testKind := "ConfigMap"

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			mpiJob := newMPIJob(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)

			cm := newConfigMap(mpiJob, 64, isGPULauncher(mpiJob))
			cm.OwnerReferences = nil
			Expect(testK8sClient.Create(ctx, cm)).Should(Succeed())

			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      mpiJob.GetName(),
			}}
			expectedErr := fmt.Errorf(MessageResourceExists, cm.Name, testKind)
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, testutil.Timeout, testutil.Interval).Should(MatchError(expectedErr))
		})
	})

	Context("MPIJob with Role not controlled by itself", func() {
		It("Should return error", func() {
			By("Calling Reconcile method")
			jobName := "test-role-orphan"
			testKind := "Role"

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			mpiJob := newMPIJob(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)

			role := newLauncherRole(mpiJob, 64)
			role.OwnerReferences = nil
			Expect(testK8sClient.Create(ctx, role)).Should(Succeed())

			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      mpiJob.GetName(),
			}}
			expectedErr := fmt.Errorf(MessageResourceExists, role.Name, testKind)
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, testutil.Timeout, testutil.Interval).Should(MatchError(expectedErr))
		})
	})

	Context("MPIJob with RoleBinding not controlled by itself", func() {
		It("Should return error", func() {
			By("Calling Reconcile method")
			jobName := "test-rb-orphan"
			testKind := "RoleBinding"

			ctx := context.Background()
			startTime := metav1.Now()
			completionTime := metav1.Now()

			mpiJob := newMPIJob(jobName, ptr.To[int32](64), 1, gpuResourceName, &startTime, &completionTime)

			rb := newLauncherRoleBinding(mpiJob)
			rb.OwnerReferences = nil
			Expect(testK8sClient.Create(ctx, rb)).Should(Succeed())

			Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      mpiJob.GetName(),
			}}
			expectedErr := fmt.Errorf(MessageResourceExists, rb.Name, testKind)
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, testutil.Timeout, testutil.Interval).Should(MatchError(expectedErr))
		})
	})

	Context("Test launcher's Intel MPI handling", func() {
		It("Should create a launcher job with Intel MPI env variables", func() {
			By("By creating MPIJobs with and without preset env variables")

			testCases := map[string]struct {
				envVariables         map[string]string
				expectedEnvVariables map[string]string
			}{
				"withoutIMPIValues": {
					envVariables: map[string]string{
						"X_MPI_HYDRA_BOOTSTRAP": "foo",
					},
					expectedEnvVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP":      iMPIDefaultBootstrap,
						"I_MPI_HYDRA_BOOTSTRAP_EXEC": fmt.Sprintf("%s/%s", configMountPath, kubexecScriptName),
					},
				},
				"withIMPIBootstrap": {
					envVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP": "RSH",
					},
					expectedEnvVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP":      "RSH",
						"I_MPI_HYDRA_BOOTSTRAP_EXEC": fmt.Sprintf("%s/%s", configMountPath, kubexecScriptName),
					},
				},
				"withIMPIBootstrapExec": {
					envVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP_EXEC": "/script.sh",
					},
					expectedEnvVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP":      iMPIDefaultBootstrap,
						"I_MPI_HYDRA_BOOTSTRAP_EXEC": "/script.sh",
					},
				},
				"withIMPIBootstrapAndExec": {
					envVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP":      "RSH",
						"I_MPI_HYDRA_BOOTSTRAP_EXEC": "/script.sh",
					},
					expectedEnvVariables: map[string]string{
						"I_MPI_HYDRA_BOOTSTRAP":      "RSH",
						"I_MPI_HYDRA_BOOTSTRAP_EXEC": "/script.sh",
					},
				},
			}

			for testName, testCase := range testCases {
				ctx := context.Background()
				startTime := metav1.Now()
				completionTime := metav1.Now()

				jobName := "test-launcher-creation-" + strings.ToLower(testName)

				mpiJob := newMPIJob(jobName, ptr.To[int32](1), 1, gpuResourceName, &startTime, &completionTime)
				Expect(testK8sClient.Create(ctx, mpiJob)).Should(Succeed())

				template := &mpiJob.Spec.MPIReplicaSpecs[kubeflowv1.MPIJobReplicaTypeLauncher].Template
				Expect(len(template.Spec.Containers) == 1).To(BeTrue())

				cont := &template.Spec.Containers[0]

				for k, v := range testCase.envVariables {
					cont.Env = append(cont.Env,
						corev1.EnvVar{
							Name:  k,
							Value: v,
						},
					)
				}

				launcher := reconciler.newLauncher(mpiJob, "kubectl-delivery", false)

				Expect(len(launcher.Spec.Containers) == 1).To(BeTrue())
				for expectedKey, expectedValue := range testCase.expectedEnvVariables {
					Expect(launcher.Spec.Containers[0].Env).Should(ContainElements(
						corev1.EnvVar{
							Name:  expectedKey,
							Value: expectedValue,
						}),
					)
				}
			}
		})
	})

	Context("When creating the MPIJob with the suspend semantics", func() {
		const name = "test-job"
		var (
			ns          *corev1.Namespace
			job         *kubeflowv1.MPIJob
			jobKey      types.NamespacedName
			launcherKey types.NamespacedName
			worker0Key  types.NamespacedName
			ctx         = context.Background()
		)
		BeforeEach(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "mpijob-test-",
				},
			}
			Expect(testK8sClient.Create(ctx, ns)).Should(Succeed())

			now := metav1.Now()
			job = newMPIJob(name, ptr.To[int32](1), 1, gpuResourceName, &now, &now)
			job.Namespace = ns.Name
			jobKey = client.ObjectKeyFromObject(job)
			launcherKey = types.NamespacedName{
				Name:      fmt.Sprintf("%s-launcher", name),
				Namespace: ns.Name,
			}
			worker0Key = types.NamespacedName{
				Name:      fmt.Sprintf("%s-worker-0", name),
				Namespace: ns.Name,
			}
		})
		AfterEach(func() {
			Expect(testK8sClient.Delete(ctx, job)).Should(Succeed())
			Expect(testK8sClient.Delete(ctx, ns)).Should(Succeed())
		})
		It("Shouldn't create resources if MPIJob is suspended", func() {
			By("By creating a new MPIJob with suspend=true")
			job.Spec.RunPolicy.Suspend = ptr.To(true)
			Expect(testK8sClient.Create(ctx, job)).Should(Succeed())

			created := &kubeflowv1.MPIJob{}
			launcherPod := &corev1.Pod{}
			workerPod := &corev1.Pod{}

			By("Checking created MPIJob")
			Eventually(func() bool {
				err := testK8sClient.Get(ctx, jobKey, created)
				return err == nil
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
			By("Checking created MPIJob has a nil startTime")
			Consistently(func() *metav1.Time {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.StartTime
			}, testutil.ConsistentDuration, testutil.Interval).Should(BeNil())

			By("Checking if the pods aren't created")
			Consistently(func() bool {
				errLauncherPod := testK8sClient.Get(ctx, launcherKey, launcherPod)
				errWorkerPod := testK8sClient.Get(ctx, worker0Key, workerPod)
				return errors.IsNotFound(errLauncherPod) && errors.IsNotFound(errWorkerPod)
			}, testutil.ConsistentDuration, testutil.Interval).Should(BeTrue())

			By("Checking if the MPIJob has suspended condition")
			Eventually(func() []kubeflowv1.JobCondition {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.Conditions
			}, testutil.ConsistentDuration, testutil.Interval).Should(BeComparableTo([]kubeflowv1.JobCondition{
				{
					Type:    kubeflowv1.JobCreated,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobCreatedReason),
					Message: fmt.Sprintf("MPIJob %s is created.", name),
				},
				{
					Type:    kubeflowv1.JobSuspended,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobSuspendedReason),
					Message: fmt.Sprintf("MPIJob %s is suspended.", name),
				},
			}, testutil.IgnoreJobConditionsTimes))
		})

		It("Should delete resources after MPIJob is suspended; Should resume MPIJob after MPIJob is unsuspended", func() {
			By("By creating a new MPIJob")
			Expect(testK8sClient.Create(ctx, job)).Should(Succeed())

			created := &kubeflowv1.MPIJob{}
			launcherPod := &corev1.Pod{}
			workerPod := &corev1.Pod{}

			// We'll need to retry getting this newly created MPIJob, given that creation may not immediately happen.
			By("Checking created MPIJob")
			Eventually(func() bool {
				err := testK8sClient.Get(ctx, jobKey, created)
				return err == nil
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())

			var startTimeBeforeSuspended *metav1.Time
			Eventually(func() *metav1.Time {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				startTimeBeforeSuspended = created.Status.StartTime
				return startTimeBeforeSuspended
			}, testutil.Timeout, testutil.Interval).ShouldNot(BeNil())

			By("Checking the created pods")
			Eventually(func() bool {
				errLauncher := testK8sClient.Get(ctx, launcherKey, launcherPod)
				errWorker := testK8sClient.Get(ctx, worker0Key, workerPod)
				return errLauncher == nil && errWorker == nil
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())

			By("Updating the Pod's phase with Running")
			Eventually(func() error {
				Expect(testK8sClient.Get(ctx, launcherKey, launcherPod)).Should(Succeed())
				launcherPod.Status.Phase = corev1.PodRunning
				return testK8sClient.Status().Update(ctx, launcherPod)
			}, testutil.Timeout, testutil.Interval).Should(Succeed())
			Eventually(func() error {
				Expect(testK8sClient.Get(ctx, worker0Key, workerPod)).Should(Succeed())
				workerPod.Status.Phase = corev1.PodRunning
				return testK8sClient.Status().Update(ctx, workerPod)
			}, testutil.Timeout, testutil.Interval).Should(Succeed())

			By("Checking the MPIJob's condition")
			Eventually(func() []kubeflowv1.JobCondition {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.Conditions
			}, testutil.Timeout, testutil.Interval).Should(BeComparableTo([]kubeflowv1.JobCondition{
				{
					Type:    kubeflowv1.JobCreated,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobCreatedReason),
					Message: fmt.Sprintf("MPIJob %s is created.", name),
				},
				{
					Type:    kubeflowv1.JobRunning,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobRunningReason),
					Message: fmt.Sprintf("MPIJob %s is running.", name),
				},
			}, testutil.IgnoreJobConditionsTimes))

			By("Updating the MPIJob with suspend=true")
			Eventually(func() error {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				created.Spec.RunPolicy.Suspend = ptr.To(true)
				return testK8sClient.Update(ctx, created)
			}, testutil.Timeout, testutil.Interval).Should(Succeed())

			By("Checking if the pods are removed")
			Eventually(func() bool {
				errLauncher := testK8sClient.Get(ctx, launcherKey, launcherPod)
				errWorker := testK8sClient.Get(ctx, worker0Key, workerPod)
				return errors.IsNotFound(errLauncher) && errors.IsNotFound(errWorker)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
			Consistently(func() bool {
				errLauncherPod := testK8sClient.Get(ctx, launcherKey, launcherPod)
				errWorkerPod := testK8sClient.Get(ctx, worker0Key, workerPod)
				return errors.IsNotFound(errLauncherPod) && errors.IsNotFound(errWorkerPod)
			}, testutil.ConsistentDuration, testutil.Interval).Should(BeTrue())

			By("Checking if the MPIJob has a suspended condition")
			Eventually(func() bool {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.ReplicaStatuses[kubeflowv1.MPIJobReplicaTypeLauncher].Active == 0 &&
					created.Status.ReplicaStatuses[kubeflowv1.MPIJobReplicaTypeWorker].Active == 0 &&
					created.Status.StartTime.Equal(startTimeBeforeSuspended)
			}, testutil.Timeout, testutil.Interval).Should(BeTrue())
			Consistently(func() bool {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.ReplicaStatuses[kubeflowv1.MPIJobReplicaTypeLauncher].Active == 0 &&
					created.Status.ReplicaStatuses[kubeflowv1.MPIJobReplicaTypeWorker].Active == 0 &&
					created.Status.StartTime.Equal(startTimeBeforeSuspended)
			}, testutil.ConsistentDuration, testutil.Interval).Should(BeTrue())
			Expect(created.Status.Conditions).Should(BeComparableTo([]kubeflowv1.JobCondition{
				{
					Type:    kubeflowv1.JobCreated,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobCreatedReason),
					Message: fmt.Sprintf("MPIJob %s is created.", name),
				},
				{
					Type:    kubeflowv1.JobRunning,
					Status:  corev1.ConditionFalse,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobSuspendedReason),
					Message: fmt.Sprintf("MPIJob %s is suspended.", name),
				},
				{
					Type:    kubeflowv1.JobSuspended,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobSuspendedReason),
					Message: fmt.Sprintf("MPIJob %s is suspended.", name),
					Status:  corev1.ConditionTrue,
				},
			}, testutil.IgnoreJobConditionsTimes))

			By("Unsuspending the MPIJob")
			Eventually(func() error {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				created.Spec.RunPolicy.Suspend = ptr.To(false)
				return testK8sClient.Update(ctx, created)
			}, testutil.Timeout, testutil.Interval).Should(Succeed())
			Eventually(func() *metav1.Time {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.StartTime
			}, testutil.Timeout, testutil.Interval).ShouldNot(BeNil())

			By("Check if the pods are created")
			Eventually(func() error {
				return testK8sClient.Get(ctx, launcherKey, launcherPod)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())
			Eventually(func() error {
				return testK8sClient.Get(ctx, worker0Key, workerPod)
			}, testutil.Timeout, testutil.Interval).Should(BeNil())

			By("Updating Pod's condition with Running")
			Eventually(func() error {
				Expect(testK8sClient.Get(ctx, launcherKey, launcherPod)).Should(Succeed())
				launcherPod.Status.Phase = corev1.PodRunning
				return testK8sClient.Status().Update(ctx, launcherPod)
			}, testutil.Timeout, testutil.Interval).Should(Succeed())
			Eventually(func() error {
				Expect(testK8sClient.Get(ctx, worker0Key, workerPod)).Should(Succeed())
				workerPod.Status.Phase = corev1.PodRunning
				return testK8sClient.Status().Update(ctx, workerPod)
			}, testutil.Timeout, testutil.Interval).Should(Succeed())

			By("Checking if the MPIJob has resumed conditions")
			Eventually(func() []kubeflowv1.JobCondition {
				Expect(testK8sClient.Get(ctx, jobKey, created)).Should(Succeed())
				return created.Status.Conditions
			}, testutil.Timeout, testutil.Interval).Should(BeComparableTo([]kubeflowv1.JobCondition{
				{
					Type:    kubeflowv1.JobCreated,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobCreatedReason),
					Message: fmt.Sprintf("MPIJob %s is created.", name),
				},
				{
					Type:    kubeflowv1.JobSuspended,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobResumedReason),
					Message: fmt.Sprintf("MPIJob %s is resumed.", name),
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    kubeflowv1.JobRunning,
					Status:  corev1.ConditionTrue,
					Reason:  commonutil.NewReason(kubeflowv1.MPIJobKind, commonutil.JobRunningReason),
					Message: fmt.Sprintf("MPIJob %s is running.", name),
				},
			}, testutil.IgnoreJobConditionsTimes))

			By("Checking if the startTime is updated")
			Expect(created.Status.StartTime).ShouldNot(Equal(startTimeBeforeSuspended))
		})
	})
})

func ReplicaStatusMatch(replicaStatuses map[common.ReplicaType]*common.ReplicaStatus,
	replicaType common.ReplicaType, status *common.ReplicaStatus) bool {

	result := true

	if replicaStatuses == nil {
		return false
	}
	if val, exist := replicaStatuses[replicaType]; !exist {
		return false
	} else {
		result = result && (val.Active == status.Active)
		result = result && (val.Succeeded == status.Succeeded)
		result = result && (val.Failed == status.Failed)
	}

	return result
}
