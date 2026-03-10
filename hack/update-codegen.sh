#!/bin/bash

# This shell is used to auto generate some useful tools for k8s, such as clientset, lister, informer and so on.
# We don't use this tool to generate deepcopy because kubebuilder (controller-tools) has covered that part.

set -o errexit
set -o nounset
set -o pipefail

CURRENT_DIR=$(dirname "${BASH_SOURCE[0]}")
TRAINING_OPERATOR_ROOT=$(realpath "${CURRENT_DIR}/..")
TRAINING_OPERATOR_PKG="github.com/kubeflow/training-operator"

cd "$CURRENT_DIR/.."

# Locate code-generator in the module cache (read-only).
CODEGEN_PKG=$(go list -m -mod=readonly -f "{{.Dir}}" k8s.io/code-generator)

# kube_codegen.sh `cd`s into its own module directory and runs `go install`
# to build the generator binaries. When that directory is the read-only
# module cache, `go install` resolves deps from the code-generator's own
# go.mod, which pins an old golang.org/x/tools incompatible with Go 1.25.
#
# Fix: copy code-generator to a writable temp directory and create a Go
# workspace that includes *both* the training-operator and the copy.
# Because the copy is a workspace member, Go's MVS merges dependency
# graphs and picks the newer, Go 1.25-compatible golang.org/x/tools from
# the training-operator's go.mod.
WORK_DIR=$(mktemp -d)
trap "chmod -R u+w ${WORK_DIR} && rm -rf ${WORK_DIR}" EXIT

CODEGEN_COPY="${WORK_DIR}/code-generator"
cp -a "${CODEGEN_PKG}" "${CODEGEN_COPY}"
chmod -R u+w "${CODEGEN_COPY}"

cat > "${WORK_DIR}/go.work" << EOF
go 1.25

use ${TRAINING_OPERATOR_ROOT}
use ${CODEGEN_COPY}
EOF
export GOWORK="${WORK_DIR}/go.work"

# Source kube_codegen.sh from the copy so that KUBE_CODEGEN_ROOT (derived
# from BASH_SOURCE) points to the workspace member, not the module cache.
source "${CODEGEN_COPY}/kube_codegen.sh"
echo ">> Using ${CODEGEN_PKG} (workspace copy at ${CODEGEN_COPY})"

# Generating deepcopy and defaults.
echo "Generating deepcopy and defaults for kubeflow.org/v1"
kube::codegen::gen_helpers \
  --boilerplate "${TRAINING_OPERATOR_ROOT}/hack/boilerplate/boilerplate.go.txt" \
  "${TRAINING_OPERATOR_ROOT}/pkg/apis"

# Generate clients for Training Operator V1
echo "Generating clients for kubeflow.org/v1"
kube::codegen::gen_client \
  --boilerplate "${TRAINING_OPERATOR_ROOT}/hack/boilerplate/boilerplate.go.txt" \
  --output-dir "${TRAINING_OPERATOR_ROOT}/pkg/client" \
  --output-pkg "${TRAINING_OPERATOR_PKG}/pkg/client" \
  --with-watch \
  --with-applyconfig \
  "${TRAINING_OPERATOR_ROOT}/pkg/apis"

# Get the kube-openapi binary.
OPENAPI_PKG=$(go list -m -mod=readonly -f "{{.Dir}}" k8s.io/kube-openapi)
echo ">> Using ${OPENAPI_PKG}"

echo "Generating OpenAPI specification for kubeflow.org/v1"
go run ${OPENAPI_PKG}/cmd/openapi-gen \
  --go-header-file "${TRAINING_OPERATOR_ROOT}/hack/boilerplate/boilerplate.go.txt" \
  --output-pkg "${TRAINING_OPERATOR_PKG}/pkg/apis/kubeflow.org/v1" \
  --output-dir "${TRAINING_OPERATOR_ROOT}/pkg/apis/kubeflow.org/v1" \
  --output-file "zz_generated.openapi.go" \
  --report-filename "${TRAINING_OPERATOR_ROOT}/hack/violation_exception_v1.list" \
  "${TRAINING_OPERATOR_ROOT}/pkg/apis/kubeflow.org/v1"
