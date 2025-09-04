# Queue Status Visibility - Testing Guide

This guide provides instructions for testing the queue status visibility enhancement in a live Kubernetes cluster.

## 📦 Deployment

### Prerequisites
- Kubernetes cluster with Flux already installed
- `kubectl` configured and connected to your cluster
- Cluster-admin permissions

### 1. Deploy Test Controller

The test image `ghcr.io/rossigee/kustomize-controller:queue-visibility` contains our queue status enhancement.

```bash
# Deploy the test controller alongside existing flux installation
kubectl apply -f test-deployment.yaml

# Verify deployment
kubectl -n flux-system get deployment kustomize-controller-queue-test
kubectl -n flux-system get pods -l app=kustomize-controller-queue-test
```

### 2. Scale Down Production Controller (Optional)

To isolate testing, you can temporarily scale down the production controller:

```bash
# Scale down production controller
kubectl -n flux-system scale deployment kustomize-controller --replicas=0

# Verify only test controller is running
kubectl -n flux-system get pods -l app.kubernetes.io/name=kustomize-controller
```

## 🧪 Test Scenarios

### Scenario 1: Basic Queue Status Visibility

**Test**: Create a new Kustomization and observe immediate status updates

```bash
# Apply test Kustomization
kubectl apply -f test-kustomization.yaml

# Immediately check status (should show queue information)
kubectl -n flux-system get kustomization queue-test-kustomization -o yaml

# Look for the new status fields:
# status:
#   observedGeneration: 0
#   queueMetadata:
#     queuedAt: "2025-01-15T10:40:00Z"
#     position: 1
#     estimatedProcessingTime: "2025-01-15T10:42:00Z"
#   conditions:
#   - type: Ready
#     status: "False" 
#     reason: "Queued"
#     message: "Resource queued for reconciliation (position 1, estimated processing in 2m)"
```

### Scenario 2: High Load Queue Behavior

**Test**: Create multiple Kustomizations to simulate queue saturation

```bash
# Create multiple test resources to fill the queue
for i in {1..10}; do
  kubectl create -f - <<EOF
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: queue-load-test-${i}
  namespace: flux-system
spec:
  interval: 1h
  sourceRef:
    kind: GitRepository
    name: queue-test-repo
  path: "./kustomize"
  prune: false
  timeout: 10m
EOF
done

# Check queue positions and estimated processing times
kubectl -n flux-system get kustomizations -o custom-columns="NAME:.metadata.name,STATUS:.status.conditions[0].reason,QUEUE_POS:.status.queueMetadata.position,QUEUED_AT:.status.queueMetadata.queuedAt"
```

### Scenario 3: Feature Flag Testing

**Test**: Verify feature flags work correctly

```bash
# Check current feature flags in controller logs
kubectl -n flux-system logs -l app=kustomize-controller-queue-test | grep -i "feature"

# Test with different feature combinations by editing the deployment:
kubectl -n flux-system edit deployment kustomize-controller-queue-test

# Modify args to test different combinations:
# Basic only: --feature-gates=QueueStatusReporting=true
# With positions: --feature-gates=QueueStatusReporting=true,QueuePositionTracking=true  
# All features: --feature-gates=QueueStatusReporting=true,QueuePositionTracking=true,QueueTimeEstimation=true
```

### Scenario 4: Status Transition Monitoring

**Test**: Monitor status transitions from queued → processing → complete

```bash
# Watch status changes in real-time
kubectl -n flux-system get kustomization queue-test-kustomization -w

# Or use a more detailed watch
watch -n 2 'kubectl -n flux-system get kustomization queue-test-kustomization -o jsonpath="{.status}" | jq'
```

## 🔍 Verification Points

### ✅ Expected Behaviors

1. **Immediate Status Updates**
   - New resources should show `observedGeneration: 0` immediately
   - Should have "Queued" condition instead of no conditions
   - Should show `queuedAt` timestamp

2. **Position Tracking** (if enabled)
   - Resources should show their position in queue
   - Positions should update as queue progresses
   - Position should be removed when processing starts

3. **Time Estimation** (if enabled)  
   - Should show estimated processing start time
   - Estimates should update as queue moves
   - Should show "processing soon" when estimate is in the past

4. **Clean Status Transitions**
   - Queue metadata should be cleared when processing begins
   - Should transition to normal Ready/Failed states after processing
   - No memory leaks or stale tracking data

### ❌ Failure Indicators

1. **Resources stuck with `observedGeneration: -1`**
2. **Missing queue metadata when features are enabled**
3. **Memory leaks in controller (check metrics)**
4. **Position numbers that don't make sense**
5. **Status updates that don't happen**

## 📊 Monitoring

### Controller Logs
```bash
# Monitor controller logs for queue status operations
kubectl -n flux-system logs -l app=kustomize-controller-queue-test -f | grep -i queue

# Look for any errors related to status updates
kubectl -n flux-system logs -l app=kustomize-controller-queue-test | grep -i error
```

### Resource Status
```bash
# Check all Kustomizations status summary
kubectl -n flux-system get kustomizations -o custom-columns="NAME:.metadata.name,OBSERVED:.status.observedGeneration,READY:.status.conditions[?(@.type=='Ready')].status,REASON:.status.conditions[?(@.type=='Ready')].reason,MESSAGE:.status.conditions[?(@.type=='Ready')].message"
```

### Memory Usage
```bash
# Monitor controller memory usage
kubectl -n flux-system top pod -l app=kustomize-controller-queue-test
```

## 🧹 Cleanup

```bash
# Remove test resources
kubectl delete -f test-kustomization.yaml
kubectl delete kustomizations -n flux-system -l "name startsWith queue-load-test"

# Remove test controller
kubectl delete -f test-deployment.yaml

# Scale production controller back up (if scaled down)
kubectl -n flux-system scale deployment kustomize-controller --replicas=1
```

## 🐛 Troubleshooting

### Controller Won't Start
- Check RBAC permissions
- Verify image availability: `docker pull ghcr.io/rossigee/kustomize-controller:queue-visibility`
- Check resource limits

### Status Not Updating
- Verify feature flags are set correctly
- Check controller logs for permission errors
- Ensure CRDs are up to date

### High Memory Usage
- Monitor queue depth with: `kubectl -n flux-system logs -l app=kustomize-controller-queue-test | grep "queue depth"`
- Check for resource leaks in status updates

## 📈 Success Metrics

A successful test should demonstrate:
- 🎯 **Immediate Visibility**: Resources show queue status within seconds of creation
- 📊 **Accurate Information**: Position and timing information is helpful and accurate
- ⚡ **Performance**: No noticeable impact on reconciliation speed
- 🧹 **Clean Behavior**: No memory leaks, proper cleanup, smooth transitions
- 🔧 **Operability**: Easy to understand status for troubleshooting

This enhancement should eliminate the confusion of `observedGeneration: -1` and provide operators with clear, actionable status information during high-load scenarios.