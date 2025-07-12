# E2E Testing Guide for Helm Undeploy Workflow

This guide explains how to run end-to-end tests locally on macOS using k3d and Temporal.

## Prerequisites

1. **k3d** - Lightweight Kubernetes distribution
   ```bash
   brew install k3d
   ```

2. **Temporal** - Workflow orchestration (installed via Homebrew)
   ```bash
   brew install temporal
   brew services start temporal
   ```

3. **Helm** - Kubernetes package manager
   ```bash
   brew install helm
   ```

4. **Go 1.21+** - For building and running the application
   ```bash
   brew install go
   ```

## Quick Start

Run the automated E2E test:
```bash
./e2e-test.sh
```

This script will:
1. Verify all prerequisites are installed
2. Create/use a k3d cluster named "helm-test"
3. Install a test Helm chart
4. Start the Temporal worker
5. Execute the undeploy workflow
6. Verify the Helm release and resources are removed
7. Clean up the worker process

## Manual Testing Steps

### 1. Start Temporal Server
```bash
# If not already running
brew services start temporal

# Verify it's running
curl http://localhost:7233
```

### 2. Create k3d Cluster
```bash
# Create cluster
k3d cluster create helm-test

# Set kubeconfig
k3d kubeconfig merge helm-test --kubeconfig-switch-context

# Verify cluster
kubectl cluster-info
```

### 3. Install Test Helm Chart
```bash
# Install the test chart
helm install my-test-app ./test-helm-chart

# Verify installation
helm list
kubectl get deployments,services -l app.kubernetes.io/instance=my-test-app
```

### 4. Start the Worker
```bash
# In one terminal
cd worker
go run main.go
```

### 5. Execute Undeploy Workflow
```bash
# In another terminal
cd test
go run main.go -release=my-test-app -namespace=default -wait=true
```

### 6. Verify Cleanup
```bash
# Check Helm releases
helm list

# Check Kubernetes resources
kubectl get all -l app.kubernetes.io/instance=my-test-app
```

## Environment Configuration

Create a `.env` file for local testing:
```bash
cp .env.example .env
```

Key environment variables:
- `TEMPORAL_HOST`: Temporal server address (default: localhost:7233)
- `TASK_QUEUE`: Queue name for workflow tasks
- `KUBECONFIG`: Path to kubeconfig file (auto-detected for k3d)

## Monitoring

### Temporal UI
View workflows in the Temporal UI:
```
http://localhost:8233
```

### Logs
The application uses structured logging with zerolog. Check worker logs for detailed execution information.

## Troubleshooting

### Temporal not running
```bash
brew services restart temporal
```

### k3d cluster issues
```bash
# Delete and recreate
k3d cluster delete helm-test
k3d cluster create helm-test
```

### Permission issues
Ensure your kubeconfig has proper permissions:
```bash
chmod 600 ~/.kube/config
```

## Cleanup

### Delete k3d cluster
```bash
k3d cluster delete helm-test
```

### Stop Temporal
```bash
brew services stop temporal
```