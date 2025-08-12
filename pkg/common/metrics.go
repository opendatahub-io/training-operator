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

package common

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	initOnce sync.Once
)

// metrics preserved for backward compatibility
var (
	jobsCreatedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "training_operator_jobs_created_total",
			Help: "Total number of training jobs created",
		},
		[]string{"job_namespace", "framework"},
	)

	jobsDeletedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "training_operator_jobs_deleted_total",
			Help: "Total number of training jobs deleted",
		},
		[]string{"job_namespace", "framework"},
	)

	jobsSuccessfulCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "training_operator_jobs_successful_total",
			Help: "Total number of successful training jobs",
		},
		[]string{"job_namespace", "framework"},
	)

	jobsFailedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "training_operator_jobs_failed_total",
			Help: "Total number of failed training jobs",
		},
		[]string{"job_namespace", "framework"},
	)

	jobsRestartedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "training_operator_jobs_restarted_total",
			Help: "Total number of restarted training jobs",
		},
		[]string{"job_namespace", "framework"},
	)
)

func init() {
	initOnce.Do(func() {
		metrics.Registry.MustRegister(
			jobsCreatedCount,
			jobsDeletedCount,
			jobsSuccessfulCount,
			jobsFailedCount,
			jobsRestartedCount,
		)
	})
}

// compatibility functions
func CreatedJobsCounterInc(jobNamespace, framework string) {
	jobsCreatedCount.WithLabelValues(jobNamespace, framework).Inc()
}

func DeletedJobsCounterInc(jobNamespace, framework string) {
	jobsDeletedCount.WithLabelValues(jobNamespace, framework).Inc()
}

func SuccessfulJobsCounterInc(jobNamespace, framework string) {
	jobsSuccessfulCount.WithLabelValues(jobNamespace, framework).Inc()
}

func FailedJobsCounterInc(jobNamespace, framework string) {
	jobsFailedCount.WithLabelValues(jobNamespace, framework).Inc()
}

func RestartedJobsCounterInc(jobNamespace, framework string) {
	jobsRestartedCount.WithLabelValues(jobNamespace, framework).Inc()
}
