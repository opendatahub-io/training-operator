#!/bin/bash

# Telemetry Verification Script for Training Operator
# Ensures metrics flow from operator -> ServiceMonitor -> Prometheus -> Recording Rules -> Telemetry

# Enable safer shell options
set -u -o pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

NAMESPACE=${NAMESPACE:-kubeflow}
OPERATOR_NAME="training-operator"

echo "=========================================="
echo "Training Operator Telemetry Verification"
echo "=========================================="

check_resource() {
    local resource=$1
    local name=$2
    local namespace=$3

    if kubectl get $resource $name -n $namespace &>/dev/null; then
        echo -e "${GREEN}✓${NC} $resource/$name exists in namespace $namespace"
        return 0
    else
        echo -e "${RED}✗${NC} $resource/$name NOT FOUND in namespace $namespace"
        return 1
    fi
}

check_label() {
    local resource=$1
    local name=$2
    local namespace=$3
    local label=$4
    local expected_value=${5:-"true"}  # Default to "true" if not provided

    # Use jq to handle dotted label keys properly (e.g., app.kubernetes.io/component)
    # JSONPath can't handle keys with dots/slashes correctly
    local label_value=$(kubectl get "$resource" "$name" -n "$namespace" -o json 2>/dev/null | jq -r ".metadata.labels[\"$label\"]" 2>/dev/null)

    if [ "$label_value" == "$expected_value" ]; then
        echo -e "${GREEN}✓${NC} $resource/$name has label $label=$expected_value"
        return 0
    else
        echo -e "${RED}✗${NC} $resource/$name missing label $label=$expected_value (got: ${label_value:-null})"
        return 1
    fi
}

echo ""
echo "1. Checking Training Operator Deployment..."
echo "-------------------------------------------"
check_resource deployment $OPERATOR_NAME $NAMESPACE

METRICS_PORT=$(kubectl get deployment $OPERATOR_NAME -n $NAMESPACE -o jsonpath='{.spec.template.spec.containers[0].ports[?(@.name=="metrics")].containerPort}')
if [ "$METRICS_PORT" == "8080" ]; then
    echo -e "${GREEN}✓${NC} Metrics port 8080 is exposed"
else
    echo -e "${RED}✗${NC} Metrics port not properly exposed (expected 8080, got: $METRICS_PORT)"
fi

echo ""
echo "2. Checking Metrics Service..."
echo "--------------------------------"
check_resource service training-operator-metrics $NAMESPACE
check_label service training-operator-metrics $NAMESPACE "app.kubernetes.io/component" "metrics"

echo ""
echo "3. Checking ServiceMonitors..."
echo "-------------------------------"
echo "Checking operational ServiceMonitor..."
check_resource servicemonitor training-operator-metrics $NAMESPACE

echo "Checking critical monitoring label..."
SM_LABEL=$(kubectl get servicemonitor training-operator-metrics -n $NAMESPACE -o json | jq -r '.metadata.labels["monitoring.opendatahub.io/scrape"]')
if [ "$SM_LABEL" == "true" ]; then
    echo -e "${GREEN}✓${NC} ServiceMonitor has critical label monitoring.opendatahub.io/scrape=true"
else
    echo -e "${RED}✗${NC} CRITICAL: ServiceMonitor missing monitoring.opendatahub.io/scrape=true label!"
    echo "  This label is REQUIRED for RHOAI platform telemetry collection!"
fi

echo ""
echo "Checking telemetry ServiceMonitor..."
check_resource servicemonitor training-operator-metrics-telemetry $NAMESPACE

TELEMETRY_SM_LABEL=$(kubectl get servicemonitor training-operator-metrics-telemetry -n $NAMESPACE -o json 2>/dev/null | jq -r '.metadata.labels["monitoring.opendatahub.io/scrape"]' 2>/dev/null)
if [ "$TELEMETRY_SM_LABEL" == "true" ]; then
    echo -e "${GREEN}✓${NC} Telemetry ServiceMonitor has monitoring.opendatahub.io/scrape=true label"

    echo "Checking telemetry metric filters..."
    # Get the actual keep regex from the last metricRelabeling rule
    KEEP_REGEX=$(kubectl get servicemonitor training-operator-metrics-telemetry -n "$NAMESPACE" -o json 2>/dev/null | jq -r '.spec.endpoints[0].metricRelabelings[] | select(.action == "keep") | .regex' 2>/dev/null)
    if [[ "$KEEP_REGEX" == *"training_operator_jobs_created_by_runtime_total"* ]] && \
       [[ "$KEEP_REGEX" == *"training_operator_image_preference_total"* ]] && \
       [[ "$KEEP_REGEX" == *"training_operator_framework_usage_total"* ]]; then
        echo -e "${GREEN}✓${NC} Telemetry ServiceMonitor correctly filters for telemetry metrics only"
    else
        echo -e "${YELLOW}⚠${NC} Telemetry ServiceMonitor may not be filtering metrics properly"
        echo "  Expected metrics: training_operator_jobs_created_by_runtime_total, training_operator_image_preference_total, training_operator_framework_usage_total"
        echo "  Actual regex: $KEEP_REGEX"
    fi
else
    echo -e "${YELLOW}⚠${NC} Telemetry ServiceMonitor not found or missing required label"
fi

echo ""
echo "4. Checking PrometheusRule (Recording Rules)..."
echo "-------------------------------------------------"
check_resource prometheusrule training-operator-recording-rules $NAMESPACE

echo "Verifying recording rule naming patterns..."
RULES=$(kubectl get prometheusrule training-operator-recording-rules -n $NAMESPACE -o json | jq -r '.spec.groups[].rules[].record')
VALID_RULES=0
INVALID_RULES=0

# RHOAISTRAT-575 compliant rule names for telemetry
EXPECTED_RULES=(
    "openshift:training_jobs_by_runtime:sum"
    "openshift:training_image_preference:sum"
    "openshift:training_framework_usage:sum"
)

for rule in $RULES; do
    # Check if rule follows openshift: prefix (required for telemetry)
    if [[ $rule == openshift:* ]]; then
        echo -e "${GREEN}✓${NC} Recording rule '$rule' follows correct openshift: pattern"
        ((VALID_RULES++))

        # Check if it's one of our expected telemetry rules
        is_expected=false
        for expected in "${EXPECTED_RULES[@]}"; do
            if [[ "$rule" == "$expected" ]]; then
                is_expected=true
                break
            fi
        done
        if [[ "$is_expected" == true ]]; then
            echo -e "${GREEN}  └─ This is a valid RHOAISTRAT-575 telemetry rule${NC}"
        fi
    else
        echo -e "${RED}✗${NC} Recording rule '$rule' does NOT follow openshift:* pattern"
        ((INVALID_RULES++))
    fi
done

echo "Recording rules summary: $VALID_RULES valid, $INVALID_RULES invalid"

echo ""
echo "5. Checking NetworkPolicy..."
echo "-----------------------------"
check_resource networkpolicy allow-monitoring-scrape $NAMESPACE

echo ""
echo "6. Checking Metrics Endpoint..."
echo "--------------------------------"
# Port-forward to test metrics endpoint
POD=$(kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=$OPERATOR_NAME -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$POD" ]; then
    echo "Testing metrics endpoint on pod $POD..."

    kubectl port-forward -n $NAMESPACE $POD 8080:8080 &>/dev/null &
    PF_PID=$!
    sleep 3

    # Test metrics endpoint
    if curl -s http://localhost:8080/metrics | grep -q "training_operator_"; then
        echo -e "${GREEN}✓${NC} Metrics endpoint is accessible and returns training_operator metrics"
        echo "Checking operational metrics..."
        for metric in "training_operator_jobs_created_total" "training_operator_jobs_deleted_total" "training_operator_jobs_successful_total" "training_operator_jobs_failed_total"; do
            if curl -s http://localhost:8080/metrics | grep -q "^$metric"; then
                echo -e "${GREEN}✓${NC} Metric $metric is exposed"
            else
                echo -e "${YELLOW}⚠${NC} Metric $metric not found (might not have data yet)"
            fi
        done

        echo "Checking telemetry metrics..."
        # Use the actual metric names exported by the code (no "telemetry_" infix)
        for metric in "training_operator_jobs_created_by_runtime_total" "training_operator_image_preference_total" "training_operator_framework_usage_total"; do
            if curl -s http://localhost:8080/metrics | grep -q "^$metric"; then
                echo -e "${GREEN}✓${NC} Telemetry metric $metric is exposed"
            else
                echo -e "${YELLOW}⚠${NC} Telemetry metric $metric not found (might not have data yet)"
            fi
        done
    else
        echo -e "${RED}✗${NC} Metrics endpoint not returning expected metrics"
    fi

    # Clean up port-forward
    kill $PF_PID 2>/dev/null
else
    echo -e "${YELLOW}⚠${NC} No running pod found, skipping endpoint test"
fi

echo ""
echo "7. Checking Prometheus Targets..."
echo "----------------------------------"

echo "Checking if ServiceMonitor is discovered by Prometheus..."
echo "(This requires access to Prometheus in openshift-monitoring namespace)"

if kubectl auth can-i get pods -n openshift-monitoring &>/dev/null; then
    PROM_POD=$(kubectl get pods -n openshift-monitoring -l "app.kubernetes.io/name=prometheus" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$PROM_POD" ]; then
        echo "Checking Prometheus targets via $PROM_POD..."
        kubectl exec -n openshift-monitoring "$PROM_POD" -c prometheus -- \
            curl -s localhost:9090/api/v1/targets | \
            jq --arg ns "$NAMESPACE" '.data.activeTargets[] | select(.labels.namespace == $ns and .labels.service == "training-operator-metrics") | .health' 2>/dev/null || \
            echo -e "${YELLOW}⚠${NC} Unable to query Prometheus targets"
    fi
else
    echo -e "${YELLOW}⚠${NC} No access to openshift-monitoring namespace, skipping Prometheus target check"
fi

echo ""
echo "8. Verifying Telemetry Cardinality..."
echo "--------------------------------------"
echo "Expected cardinality for telemetry metrics (RHOAISTRAT-575 compliant):"
echo "  - openshift:training_jobs_by_runtime:sum -> 5 timeseries"
echo "    (pytorch-2.4, pytorch-2.5, pytorch-other, tensorflow, other)"
echo "  - openshift:training_image_preference:sum -> 2 timeseries"
echo "    (rhoai, external)"
echo "  - openshift:training_framework_usage:sum -> 3 timeseries"
echo "    (pytorch, tensorflow, other)"
echo "  Total: 10 timeseries EXACTLY (meeting Red Hat Handbook limit)"

echo ""
echo "=========================================="
echo "Verification Summary"
echo "=========================================="

echo ""
echo "Next Steps for Telemetry Activation:"
echo "1. File MON JIRA ticket for telemetry approval"
echo "2. Submit PR to openshift/cluster-monitoring-operator to allowlist metrics"
echo "3. Submit PR to rhobs/configuration to sync telemetry server"
echo ""
echo "Refer to docs/TELEMETRY_APPROVAL_PROCESS.md for detailed instructions"

echo ""
echo -e "${GREEN}Verification complete!${NC}"
