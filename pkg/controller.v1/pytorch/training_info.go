// Copyright 2025 The Kubeflow Authors
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

package pytorch

import (
	"fmt"

	kubeflowv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
)

const (
	// EnvTrainingProgressFilePath is the environment variable name for the training progress file path.
	EnvTrainingProgressFilePath = "TRAINING_PROGRESS_FILE_PATH"
)

func GetProgressFilePath(job *kubeflowv1.PyTorchJob) string {
	return fmt.Sprintf("/tmp/training_data/%s/progress.json", job.Name)
}
