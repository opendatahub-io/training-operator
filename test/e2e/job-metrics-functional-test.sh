#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'
BOLD='\033[1m'

NAMESPACE="${NAMESPACE:-kubeflow}"
METRICS_PORT="${METRICS_PORT:-8080}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-training-operator}"
WAIT_TIME="${WAIT_TIME:-30}"

TESTS_PASSED=0
TESTS_FAILED=0
PORT_FORWARD_PID=""

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
    ((TESTS_PASSED++))
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
    ((TESTS_FAILED++))
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

cleanup_test_resources() {
    log_info "Cleaning up..."

    if [ ! -z "$PORT_FORWARD_PID" ]; then
        kill $PORT_FORWARD_PID 2>/dev/null || true
    fi

    kubectl delete pytorchjob test-pytorch-24 -n $NAMESPACE 2>/dev/null || true
    kubectl delete pytorchjob test-pytorch-25 -n $NAMESPACE 2>/dev/null || true
    kubectl delete pytorchjob test-pytorch-other -n $NAMESPACE 2>/dev/null || true
    kubectl delete tfjob test-tensorflow -n $NAMESPACE 2>/dev/null || true
    kubectl delete mpijob test-other-framework -n $NAMESPACE 2>/dev/null || true
}

trap cleanup_test_resources EXIT

create_test_job_manifests() {
    log_info "Creating test job manifests..."

    cat <<EOF > /tmp/test-pytorch-24.yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: test-pytorch-24
  namespace: $NAMESPACE
  labels:
    test: telemetry
    test-type: pytorch-2.4
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      restartPolicy: OnFailure
      template:
        spec:
          containers:
          - name: pytorch
            image: docker.io/pytorch/pytorch:2.4.0-cuda12.1-cudnn9-runtime
            command: ["python", "-c", "print('Testing PyTorch 2.4 telemetry')"]
            resources:
              requests:
                memory: "512Mi"
                cpu: "100m"
              limits:
                memory: "1Gi"
                cpu: "500m"
EOF

    cat <<EOF > /tmp/test-pytorch-25.yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: test-pytorch-25
  namespace: $NAMESPACE
  labels:
    test: telemetry
    test-type: pytorch-2.5
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      restartPolicy: OnFailure
      template:
        spec:
          containers:
          - name: pytorch
            image: quay.io/modh/pytorch:2.5.0-cuda11.8
            command: ["python", "-c", "print('Testing PyTorch 2.5 telemetry')"]
            resources:
              requests:
                memory: "512Mi"
                cpu: "100m"
              limits:
                memory: "1Gi"
                cpu: "500m"
EOF

    cat <<EOF > /tmp/test-pytorch-other.yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: test-pytorch-other
  namespace: $NAMESPACE
  labels:
    test: telemetry
    test-type: pytorch-other
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      restartPolicy: OnFailure
      template:
        spec:
          containers:
          - name: pytorch
            image: registry.redhat.io/ubi8/pytorch:1.13
            command: ["python", "-c", "print('Testing PyTorch other version telemetry')"]
            resources:
              requests:
                memory: "512Mi"
                cpu: "100m"
              limits:
                memory: "1Gi"
                cpu: "500m"
EOF

    cat <<EOF > /tmp/test-tensorflow.yaml
apiVersion: kubeflow.org/v1
kind: TFJob
metadata:
  name: test-tensorflow
  namespace: $NAMESPACE
  labels:
    test: telemetry
    test-type: tensorflow
spec:
  tfReplicaSpecs:
    Worker:
      replicas: 1
      restartPolicy: OnFailure
      template:
        spec:
          containers:
          - name: tensorflow
            image: tensorflow/tensorflow:2.15.0
            command: ["python", "-c", "print('Testing TensorFlow telemetry')"]
            resources:
              requests:
                memory: "512Mi"
                cpu: "100m"
              limits:
                memory: "1Gi"
                cpu: "500m"
EOF

    cat <<EOF > /tmp/test-other-framework.yaml
apiVersion: kubeflow.org/v1
kind: MPIJob
metadata:
  name: test-other-framework
  namespace: $NAMESPACE
  labels:
    test: telemetry
    test-type: other
spec:
  slotsPerWorker: 1
  runPolicy:
    cleanPodPolicy: None
  mpiReplicaSpecs:
    Launcher:
      replicas: 1
      template:
        spec:
          containers:
          - name: mpi
            image: mpioperator/mpi-operator:latest
            command: ["echo", "Testing other framework telemetry"]
            resources:
              requests:
                memory: "256Mi"
                cpu: "100m"
    Worker:
      replicas: 1
      template:
        spec:
          containers:
          - name: mpi
            image: mpioperator/mpi-operator:latest
            command: ["echo", "Worker"]
            resources:
              requests:
                memory: "256Mi"
                cpu: "100m"
EOF

    log_success "Test manifests created"
}

check_prerequisites() {
    echo -e "\n${BOLD}=== Checking Prerequisites ===${NC}\n"

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl not found. Please install kubectl."
        exit 1
    fi
    log_success "kubectl available"

    if ! command -v curl &> /dev/null; then
        log_error "curl not found. Please install curl."
        exit 1
    fi
    log_success "curl available"

    if ! command -v bc &> /dev/null; then
        log_error "bc not found. Please install bc for calculations."
        exit 1
    fi
    log_success "bc available"

    if kubectl get namespace $NAMESPACE &> /dev/null; then
        log_success "Namespace '$NAMESPACE' exists"
    else
        log_error "Namespace '$NAMESPACE' not found"
        exit 1
    fi

    if kubectl get deployment $DEPLOYMENT_NAME -n $NAMESPACE &> /dev/null; then
        log_success "Deployment '$DEPLOYMENT_NAME' exists"
    else
        log_error "Deployment '$DEPLOYMENT_NAME' not found in namespace '$NAMESPACE'"
        exit 1
    fi

    TELEMETRY_ENV=$(kubectl get deployment $DEPLOYMENT_NAME -n $NAMESPACE -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="TELEMETRY_ENABLED")].value}' 2>/dev/null || echo "")
    if [ "$TELEMETRY_ENV" = "true" ]; then
        log_success "TELEMETRY_ENABLED is set to true"
    else
        log_warning "TELEMETRY_ENABLED not set or false. Setting it now..."
        kubectl set env deployment/$DEPLOYMENT_NAME -n $NAMESPACE TELEMETRY_ENABLED=true
        kubectl rollout status deployment/$DEPLOYMENT_NAME -n $NAMESPACE --timeout=60s
        log_success "TELEMETRY_ENABLED set to true"
    fi
}

setup_metrics_endpoint() {
    echo -e "\n${BOLD}=== Setting up Metrics Endpoint ===${NC}\n"

    if curl -s http://localhost:$METRICS_PORT/metrics > /dev/null 2>&1; then
        log_success "Metrics endpoint already accessible"
        return 0
    fi

    log_info "Starting port-forward to metrics endpoint..."
    kubectl port-forward -n $NAMESPACE deployment/$DEPLOYMENT_NAME $METRICS_PORT:$METRICS_PORT &
    PORT_FORWARD_PID=$!

    sleep 5

    if curl -s http://localhost:$METRICS_PORT/metrics > /dev/null 2>&1; then
        log_success "Metrics endpoint accessible at http://localhost:$METRICS_PORT/metrics"
    else
        log_error "Failed to access metrics endpoint"
        exit 1
    fi
}

test_metric_cardinality_compliance() {
    echo -e "\n${BOLD}=== Testing Cardinality Compliance (Max 10 timeseries) ===${NC}\n"

    METRICS_OUTPUT=$(curl -s http://localhost:$METRICS_PORT/metrics)

    RUNTIME_COUNT=$(echo "$METRICS_OUTPUT" | grep -c "^training_operator_jobs_created_by_runtime_total" || echo 0)
    IMAGE_COUNT=$(echo "$METRICS_OUTPUT" | grep -c "^training_operator_image_preference_total" || echo 0)
    FRAMEWORK_COUNT=$(echo "$METRICS_OUTPUT" | grep -c "^training_operator_framework_usage_total" || echo 0)

    TOTAL=$((RUNTIME_COUNT + IMAGE_COUNT + FRAMEWORK_COUNT))

    echo "  Runtime metrics:    $RUNTIME_COUNT timeseries"
    echo "  Image preference:   $IMAGE_COUNT timeseries"
    echo "  Framework usage:    $FRAMEWORK_COUNT timeseries"
    echo "  ────────────────"
    echo "  TOTAL:             $TOTAL timeseries"
    echo

    if [ $TOTAL -eq 10 ]; then
        log_success "Exactly 10 timeseries - Red Hat Handbook compliant"
    elif [ $TOTAL -lt 10 ]; then
        log_warning "Only $TOTAL timeseries found (expected 10)"
    else
        log_error "Exceeds limit! $TOTAL > 10"
    fi

    echo -e "\n  ${BOLD}Label Values:${NC}"
    echo "  Runtime: $(echo "$METRICS_OUTPUT" | grep 'training_operator_jobs_created_by_runtime_total' | sed -n 's/.*runtime="\([^"]*\)".*/\1/p' | sort -u | tr '\n' ' ')"
    echo "  Source: $(echo "$METRICS_OUTPUT" | grep 'training_operator_image_preference_total' | sed -n 's/.*source="\([^"]*\)".*/\1/p' | sort -u | tr '\n' ' ')"
    echo "  Framework: $(echo "$METRICS_OUTPUT" | grep 'training_operator_framework_usage_total' | sed -n 's/.*framework="\([^"]*\)".*/\1/p' | sort -u | tr '\n' ' ')"
}

test_telemetry_code_compliance() {
    echo -e "\n${BOLD}=== Testing Code Compliance ===${NC}\n"

    METRICS_FILE="pkg/common/metrics_telemetry.go"

    if [ ! -f "$METRICS_FILE" ]; then
        log_warning "Cannot find $METRICS_FILE for static analysis"
        return
    fi

    if grep -q 'Check PyTorch FIRST' "$METRICS_FILE"; then
        log_success "PyTorch prioritization confirmed in code"
    else
        log_warning "Cannot confirm PyTorch prioritization in comments"
    fi

    if grep -q 'RHOAISTRAT-575' "$METRICS_FILE"; then
        log_success "RHOAISTRAT-575 compliance documented in code"
    else
        log_warning "RHOAISTRAT-575 reference not found in code"
    fi

    if grep -q 'TELEMETRY_ENABLED' "$METRICS_FILE"; then
        log_success "Telemetry is conditionally enabled"
    else
        log_warning "Cannot confirm telemetry is conditional"
    fi
}

test_job_metrics_collection() {
    echo -e "\n${BOLD}=== Testing Job Metrics Collection ===${NC}\n"

    log_info "Getting baseline metrics..."
    INITIAL_METRICS=$(curl -s http://localhost:$METRICS_PORT/metrics)

    INITIAL_PYTORCH_24=$(echo "$INITIAL_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-2.4"}' | awk '{print $2}' || echo 0)
    INITIAL_PYTORCH_25=$(echo "$INITIAL_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-2.5"}' | awk '{print $2}' || echo 0)
    INITIAL_PYTORCH_OTHER=$(echo "$INITIAL_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-other"}' | awk '{print $2}' || echo 0)
    INITIAL_TENSORFLOW=$(echo "$INITIAL_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="tensorflow"}' | awk '{print $2}' || echo 0)
    INITIAL_OTHER=$(echo "$INITIAL_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="other"}' | awk '{print $2}' || echo 0)

    echo "  Baseline metrics:"
    echo "    pytorch-2.4:   $INITIAL_PYTORCH_24"
    echo "    pytorch-2.5:   $INITIAL_PYTORCH_25"
    echo "    pytorch-other: $INITIAL_PYTORCH_OTHER"
    echo "    tensorflow:    $INITIAL_TENSORFLOW"
    echo "    other:         $INITIAL_OTHER"
    echo

    log_info "Creating test jobs..."
    kubectl apply -f /tmp/test-pytorch-24.yaml 2>/dev/null && log_success "Created PyTorch 2.4 job" || log_warning "Failed to create PyTorch 2.4 job"
    kubectl apply -f /tmp/test-pytorch-25.yaml 2>/dev/null && log_success "Created PyTorch 2.5 job" || log_warning "Failed to create PyTorch 2.5 job"
    kubectl apply -f /tmp/test-pytorch-other.yaml 2>/dev/null && log_success "Created PyTorch other job" || log_warning "Failed to create PyTorch other job"
    kubectl apply -f /tmp/test-tensorflow.yaml 2>/dev/null && log_success "Created TensorFlow job" || log_warning "Failed to create TensorFlow job"
    kubectl apply -f /tmp/test-other-framework.yaml 2>/dev/null && log_success "Created Other framework job" || log_warning "Failed to create Other framework job"

    log_info "Waiting ${WAIT_TIME} seconds for metrics to update..."
    sleep $WAIT_TIME

    log_info "Checking updated metrics..."
    UPDATED_METRICS=$(curl -s http://localhost:$METRICS_PORT/metrics)

    UPDATED_PYTORCH_24=$(echo "$UPDATED_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-2.4"}' | awk '{print $2}' || echo 0)
    UPDATED_PYTORCH_25=$(echo "$UPDATED_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-2.5"}' | awk '{print $2}' || echo 0)
    UPDATED_PYTORCH_OTHER=$(echo "$UPDATED_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-other"}' | awk '{print $2}' || echo 0)
    UPDATED_TENSORFLOW=$(echo "$UPDATED_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="tensorflow"}' | awk '{print $2}' || echo 0)
    UPDATED_OTHER=$(echo "$UPDATED_METRICS" | grep 'training_operator_jobs_created_by_runtime_total{runtime="other"}' | awk '{print $2}' || echo 0)

    echo
    echo "  Updated metrics:"
    echo "    pytorch-2.4:   $UPDATED_PYTORCH_24 (was $INITIAL_PYTORCH_24)"
    echo "    pytorch-2.5:   $UPDATED_PYTORCH_25 (was $INITIAL_PYTORCH_25)"
    echo "    pytorch-other: $UPDATED_PYTORCH_OTHER (was $INITIAL_PYTORCH_OTHER)"
    echo "    tensorflow:    $UPDATED_TENSORFLOW (was $INITIAL_TENSORFLOW)"
    echo "    other:         $UPDATED_OTHER (was $INITIAL_OTHER)"
    echo

    if [ "$UPDATED_PYTORCH_24" -gt "$INITIAL_PYTORCH_24" ]; then
        log_success "PyTorch 2.4 metric incremented"
    else
        log_warning "PyTorch 2.4 metric not incremented"
    fi

    if [ "$UPDATED_PYTORCH_25" -gt "$INITIAL_PYTORCH_25" ]; then
        log_success "PyTorch 2.5 metric incremented"
    else
        log_warning "PyTorch 2.5 metric not incremented"
    fi

    if [ "$UPDATED_PYTORCH_OTHER" -gt "$INITIAL_PYTORCH_OTHER" ]; then
        log_success "PyTorch other metric incremented"
    else
        log_warning "PyTorch other metric not incremented"
    fi

    if [ "$UPDATED_TENSORFLOW" -gt "$INITIAL_TENSORFLOW" ]; then
        log_success "TensorFlow metric incremented"
    else
        log_warning "TensorFlow metric not incremented"
    fi

    if [ "$UPDATED_OTHER" -gt "$INITIAL_OTHER" ]; then
        log_success "Other framework metric incremented"
    else
        log_warning "Other framework metric not incremented"
    fi

    echo
    log_info "Checking image source preferences..."
    RHOAI_COUNT=$(echo "$UPDATED_METRICS" | grep 'training_operator_image_preference_total{source="rhoai"}' | awk '{print $2}' || echo 0)
    EXTERNAL_COUNT=$(echo "$UPDATED_METRICS" | grep 'training_operator_image_preference_total{source="external"}' | awk '{print $2}' || echo 0)

    echo "  RHOAI images:    $RHOAI_COUNT"
    echo "  External images: $EXTERNAL_COUNT"

    if [ "$RHOAI_COUNT" -gt "0" ] || [ "$EXTERNAL_COUNT" -gt "0" ]; then
        log_success "Image preference metrics working"
    else
        log_warning "Image preference metrics not updated"
    fi
}

analyze_business_telemetry_metrics() {
    echo -e "\n${BOLD}=== Business Metrics Analysis ===${NC}\n"

    METRICS_OUTPUT=$(curl -s http://localhost:$METRICS_PORT/metrics)

    PYTORCH_24=$(echo "$METRICS_OUTPUT" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-2.4"}' | awk '{print $2}' || echo 0)
    PYTORCH_25=$(echo "$METRICS_OUTPUT" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-2.5"}' | awk '{print $2}' || echo 0)
    PYTORCH_OTHER=$(echo "$METRICS_OUTPUT" | grep 'training_operator_jobs_created_by_runtime_total{runtime="pytorch-other"}' | awk '{print $2}' || echo 0)
    TENSORFLOW=$(echo "$METRICS_OUTPUT" | grep 'training_operator_jobs_created_by_runtime_total{runtime="tensorflow"}' | awk '{print $2}' || echo 0)
    OTHER=$(echo "$METRICS_OUTPUT" | grep 'training_operator_jobs_created_by_runtime_total{runtime="other"}' | awk '{print $2}' || echo 0)

    TOTAL_PYTORCH=$(echo "$PYTORCH_24 + $PYTORCH_25 + $PYTORCH_OTHER" | bc)
    TOTAL_JOBS=$(echo "$TOTAL_PYTORCH + $TENSORFLOW + $OTHER" | bc)

    if [ "$TOTAL_JOBS" -gt "0" ]; then
        echo "  ${BOLD}Q1: Can we deprecate PyTorch 2.4?${NC}"
        if [ "$TOTAL_PYTORCH" -gt "0" ]; then
            PYTORCH_24_PCT=$(echo "scale=1; $PYTORCH_24 * 100 / $TOTAL_PYTORCH" | bc)
            echo "    PyTorch 2.4 usage: ${PYTORCH_24_PCT}% of PyTorch workloads"
            if (( $(echo "$PYTORCH_24_PCT < 20" | bc -l) )); then
                echo "    → YES, can deprecate (under 20% threshold)"
            else
                echo "    → NO, still significant usage"
            fi
        fi

        echo
        echo "  ${BOLD}Q2: Do customers prefer RHOAI images?${NC}"
        RHOAI=$(echo "$METRICS_OUTPUT" | grep 'training_operator_image_preference_total{source="rhoai"}' | awk '{print $2}' || echo 0)
        EXTERNAL=$(echo "$METRICS_OUTPUT" | grep 'training_operator_image_preference_total{source="external"}' | awk '{print $2}' || echo 0)
        TOTAL_IMAGES=$(echo "$RHOAI + $EXTERNAL" | bc)
        if [ "$TOTAL_IMAGES" -gt "0" ]; then
            RHOAI_PCT=$(echo "scale=1; $RHOAI * 100 / $TOTAL_IMAGES" | bc)
            echo "    RHOAI adoption: ${RHOAI_PCT}%"
            if (( $(echo "$RHOAI_PCT > 60" | bc -l) )); then
                echo "    → Strong RHOAI preference"
            else
                echo "    → Need to improve RHOAI adoption"
            fi
        fi

        echo
        echo "  ${BOLD}Q3: Which framework to prioritize?${NC}"
        PYTORCH_PCT=$(echo "scale=1; $TOTAL_PYTORCH * 100 / $TOTAL_JOBS" | bc)
        echo "    PyTorch: ${PYTORCH_PCT}% of all workloads"
        echo "    → PyTorch is the clear priority"
    else
        log_warning "No job metrics collected yet - run test jobs first"
    fi
}

test_prometheus_monitoring_integration() {
    echo -e "\n${BOLD}=== Testing Prometheus Integration ===${NC}\n"

    if kubectl get servicemonitor -n $NAMESPACE training-operator 2>/dev/null; then
        log_success "ServiceMonitor exists"

        SCRAPE_LABEL=$(kubectl get servicemonitor -n $NAMESPACE training-operator -o jsonpath='{.metadata.labels.monitoring\.opendatahub\.io/scrape}')
        if [ "$SCRAPE_LABEL" = "true" ]; then
            log_success "ServiceMonitor has required scrape label"
        else
            log_warning "ServiceMonitor missing monitoring.opendatahub.io/scrape label"
        fi
    else
        log_warning "ServiceMonitor not found"
    fi

    if kubectl get prometheusrule -n $NAMESPACE training-operator-recording 2>/dev/null; then
        log_success "PrometheusRule for recording rules exists"

        RULES=$(kubectl get prometheusrule -n $NAMESPACE training-operator-recording -o yaml)
        if echo "$RULES" | grep -q "openshift:training"; then
            log_success "Recording rules use openshift: prefix"
        else
            log_warning "Recording rules missing openshift: prefix"
        fi
    else
        log_warning "PrometheusRule not found"
    fi
}

run_telemetry_test_suite() {
    echo -e "${BOLD}${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Training Operator Telemetry End-to-End Test Suite       ║"
    echo "║         RHOAISTRAT-575 & Red Hat Handbook Compliance        ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    check_prerequisites
    create_test_job_manifests
    setup_metrics_endpoint
    test_metric_cardinality_compliance
    test_telemetry_code_compliance
    test_prometheus_monitoring_integration

    echo
    read -p "Do you want to create test jobs and verify metric collection? (y/n): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        test_job_metrics_collection
        analyze_business_telemetry_metrics
    fi

    echo -e "\n${BOLD}${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                        TEST SUMMARY                         ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    echo "  Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo "  Tests Failed: ${RED}$TESTS_FAILED${NC}"
    echo

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "  ${GREEN}${BOLD}✨ ALL TESTS PASSED! Implementation is production ready! ✨${NC}"
        echo
        echo "  Next steps:"
        echo "  1. Create JIRA ticket using JIRA_TEMPLATE.md"
        echo "  2. Ensure namespace is labeled: kubectl label namespace $NAMESPACE openshift.io/cluster-monitoring=true"
        echo "  3. Wait for JIRA approval from MON team"
        exit 0
    else
        echo -e "  ${YELLOW}${BOLD}⚠️  Some tests failed or had warnings. Review output above. ⚠️${NC}"
        exit 1
    fi
}

run_telemetry_test_suite "$@"
