#!/bin/bash
set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "=== Helm Undeploy E2E Test Script ==="
echo ""

# Configuration
RELEASE_NAME="test-app-$(date +%s)"
NAMESPACE="default"
K3D_CLUSTER="helm-test"

# Function to print colored output
print_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✓ $2${NC}"
    else
        echo -e "${RED}✗ $2${NC}"
        exit 1
    fi
}

# Check prerequisites
echo "Checking prerequisites..."

# Check if k3d is installed
if ! command -v k3d &> /dev/null; then
    echo "k3d is not installed. Please install k3d first."
    exit 1
fi
print_status 0 "k3d is installed"

# Check if temporal is running (via brew)
if ! nc -zv localhost 7233 &> /dev/null; then
    echo "Temporal server is not running on localhost:7233"
    echo "Please start Temporal using: brew services start temporal"
    exit 1
fi
print_status 0 "Temporal server is running"

# Check if helm is installed
if ! command -v helm &> /dev/null; then
    echo "helm is not installed. Please install helm first."
    exit 1
fi
print_status 0 "helm is installed"

echo ""
echo "Setting up test environment..."

# Create k3d cluster if it doesn't exist
if ! k3d cluster list | grep -q "$K3D_CLUSTER"; then
    echo "Creating k3d cluster: $K3D_CLUSTER"
    k3d cluster create $K3D_CLUSTER --servers 1 --agents 1
    print_status $? "Created k3d cluster"
else
    echo "Using existing k3d cluster: $K3D_CLUSTER"
fi

# Set kubeconfig context
k3d kubeconfig merge $K3D_CLUSTER --kubeconfig-switch-context
print_status $? "Set kubeconfig context"

# Wait for cluster to be ready
echo "Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=60s
print_status $? "Cluster is ready"

echo ""
echo "Running E2E test..."

# Step 1: Install test helm chart
echo "1. Installing test Helm chart..."
helm install $RELEASE_NAME ./test-helm-chart -n $NAMESPACE --create-namespace --wait
print_status $? "Helm chart installed: $RELEASE_NAME"

# Verify deployment
kubectl get deployment -n $NAMESPACE -l app.kubernetes.io/instance=$RELEASE_NAME
kubectl get service -n $NAMESPACE -l app.kubernetes.io/instance=$RELEASE_NAME

# Step 2: Start the worker
echo ""
echo "2. Starting Temporal worker..."
cd worker
go build -o helm-undeploy-worker
./helm-undeploy-worker &
WORKER_PID=$!
sleep 3
print_status 0 "Worker started (PID: $WORKER_PID)"
cd ..

# Step 3: Execute the workflow to undeploy
echo ""
echo "3. Executing undeploy workflow..."
cd test
go run main.go \
    -release="$RELEASE_NAME" \
    -namespace="$NAMESPACE" \
    -wait=true \
    -timeout=2m \
    -workflow-id="e2e-test-$RELEASE_NAME"
WORKFLOW_RESULT=$?
cd ..
print_status $WORKFLOW_RESULT "Workflow executed"

# Step 4: Verify helm release is removed
echo ""
echo "4. Verifying helm release is removed..."
if helm list -n $NAMESPACE | grep -q $RELEASE_NAME; then
    echo "Release $RELEASE_NAME still exists!"
    exit 1
fi
print_status 0 "Helm release removed"

# Step 5: Verify kubernetes resources are removed
echo ""
echo "5. Verifying Kubernetes resources are removed..."
DEPLOYMENTS=$(kubectl get deployment -n $NAMESPACE -l app.kubernetes.io/instance=$RELEASE_NAME -o name 2>/dev/null | wc -l)
SERVICES=$(kubectl get service -n $NAMESPACE -l app.kubernetes.io/instance=$RELEASE_NAME -o name 2>/dev/null | wc -l)

if [ $DEPLOYMENTS -gt 0 ] || [ $SERVICES -gt 0 ]; then
    echo "Some resources still exist!"
    echo "Deployments: $DEPLOYMENTS"
    echo "Services: $SERVICES"
    exit 1
fi
print_status 0 "All Kubernetes resources removed"

# Cleanup
echo ""
echo "Cleaning up..."
kill $WORKER_PID 2>/dev/null || true
print_status 0 "Worker stopped"

echo ""
echo -e "${GREEN}=== E2E Test Completed Successfully ===${NC}"
echo ""
echo "To delete the k3d cluster, run:"
echo "  k3d cluster delete $K3D_CLUSTER"