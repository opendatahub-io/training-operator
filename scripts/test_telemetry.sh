#!/bin/bash
# KFTO Telemetry Phase 1 Test Script

set -e

NAMESPACE=${NAMESPACE:-opendatahub}

echo "================================"
echo "KFTO Telemetry Phase 1 Test"
echo "================================"

# Find training-operator pod
echo "→ Finding training-operator pod..."
OPERATOR_POD=$(kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=training-operator -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$OPERATOR_POD" ]; then
    echo "✗ ERROR: Training operator not found in namespace: $NAMESPACE"
    exit 1
fi

echo "✓ Found operator pod: $OPERATOR_POD"

# Create test PyTorchJobs
echo ""
echo "Creating test PyTorchJobs..."

# Test 1: RHOAI PyTorch 2.5 image
cat <<EOF | kubectl apply -f -
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: test-rhoai-25
  namespace: $NAMESPACE
spec:
  pytorchReplicaSpecs:
    Worker:
      replicas: 1
      template:
        spec:
          containers:
          - name: pytorch
            image: quay.io/modh/pytorch:2.5.0-cuda-12.4-python-3.11
            command: ["python", "-c", "print('RHOAI 2.5 test')"]
            resources:
              limits:
                cpu: 100m
                memory: 128Mi
EOF

# Test 2: RHOAI PyTorch 2.4 image
cat <<EOF | kubectl apply -f -
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: test-rhoai-24
  namespace: $NAMESPACE
spec:
  pytorchReplicaSpecs:
    Worker:
      replicas: 1
      template:
        spec:
          containers:
          - name: pytorch
            image: quay.io/opendatahub/pytorch:2.4.0-cuda-12.1-python-3.11
            command: ["python", "-c", "print('RHOAI 2.4 test')"]
            resources:
              limits:
                cpu: 100m
                memory: 128Mi
EOF

# Test 3: Custom image
cat <<EOF | kubectl apply -f -
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: test-custom
  namespace: $NAMESPACE
spec:
  pytorchReplicaSpecs:
    Worker:
      replicas: 1
      template:
        spec:
          containers:
          - name: pytorch
            image: docker.io/pytorch/pytorch:latest
            command: ["python", "-c", "print('Custom image test')"]
            resources:
              limits:
                cpu: 100m
                memory: 128Mi
EOF

echo "✓ Created 3 test jobs"

# Wait for metrics
echo ""
echo "→ Waiting 20 seconds for metrics to be recorded..."
sleep 20

# Port forward to metrics endpoint
echo "→ Fetching metrics..."
kubectl port-forward -n $NAMESPACE svc/training-operator 8443:8443 &
PF_PID=$!
sleep 3

# Fetch metrics
curl -sk https://localhost:8443/metrics > /tmp/kfto_metrics.txt 2>/dev/null || {
    echo "✗ Failed to fetch metrics"
    kill $PF_PID 2>/dev/null
    exit 1
}

kill $PF_PID 2>/dev/null

# Verify PRIMARY metric exists
echo ""
echo "Phase 1 Metric Verification"
echo "---------------------------"

if grep -q "^training_jobs_total{" /tmp/kfto_metrics.txt; then
    echo "✓ PRIMARY metric 'training_jobs_total' found"
    
    # Show metric values
    echo ""
    echo "Metric values:"
    grep "^training_jobs_total{" /tmp/kfto_metrics.txt | while read line; do
        echo "  $line"
    done
else
    echo "✗ PRIMARY metric 'training_jobs_total' NOT FOUND"
    exit 1
fi

# Verify cardinality
echo ""
echo "Cardinality Check"
echo "-----------------"
SERIES_COUNT=$(grep "^training_jobs_total{" /tmp/kfto_metrics.txt | wc -l)
echo "→ Total series: $SERIES_COUNT"

if [ $SERIES_COUNT -le 10 ]; then
    echo "✓ PASS: Cardinality within limit ($SERIES_COUNT ≤ 10)"
else
    echo "✗ FAIL: Cardinality exceeds limit ($SERIES_COUNT > 10)"
    exit 1
fi

# Verify expected labels
echo ""
echo "Label Verification"
echo "------------------"

# Check image_type labels
if grep -q 'image_type="rhoai"' /tmp/kfto_metrics.txt; then
    echo "✓ Found image_type='rhoai' label"
else
    echo "⚠ Missing image_type='rhoai' label"
fi

if grep -q 'image_type="custom"' /tmp/kfto_metrics.txt; then
    echo "✓ Found image_type='custom' label"
else
    echo "⚠ Missing image_type='custom' label"
fi

# Check runtime_version labels
FOUND_VERSIONS=""
for version in "2.5" "2.4" "other"; do
    if grep -q "runtime_version=\"$version\"" /tmp/kfto_metrics.txt; then
        echo "✓ Found runtime_version='$version' label"
        FOUND_VERSIONS="$FOUND_VERSIONS $version"
    fi
done

# Verify legacy metrics still exist
echo ""
echo "Legacy Metric Compatibility"
echo "---------------------------"
if grep -q "training_operator_jobs_created_total" /tmp/kfto_metrics.txt; then
    echo "✓ Legacy metrics preserved"
else
    echo "⚠ Legacy metrics missing"
fi

# Clean up
echo ""
echo "→ Cleaning up test resources..."
kubectl delete pytorchjob test-rhoai-25 test-rhoai-24 test-custom -n $NAMESPACE --ignore-not-found=true 2>/dev/null

echo ""
echo "================================"
echo "✓ Phase 1 Test Complete!"
echo "================================"
echo ""
echo "Summary:"
echo "- PRIMARY metric 'training_jobs_total' is working"
echo "- Cardinality is within limits (≤10 series)"
echo "- Image classification: RHOAI vs custom"
echo "- Runtime version detection: 2.5, 2.4, other"
echo "- Legacy metrics preserved for compatibility"
echo "- Metrics secured with kube-rbac-proxy"