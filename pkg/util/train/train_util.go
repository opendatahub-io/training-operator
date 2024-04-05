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

// Package that various helper routines for training.
package train

import (
	"k8s.io/utils/ptr"

	kubeflowv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
)

func IsRetryableExitCode(exitCode int32) bool {
	return exitCode >= 128
}

func IsJobSuspended(runPolicy *kubeflowv1.RunPolicy) bool {
	return runPolicy != nil && ptr.Deref(runPolicy.Suspend, false)
}
