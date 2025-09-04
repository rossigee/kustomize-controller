# Queue Status Visibility Implementation

This document describes the implementation of enhanced queue status visibility for the kustomize-controller, addressing the UX issue where resources appear "invisible" during high load conditions.

## Problem Statement

During high controller load, new Kustomization resources would show:
```yaml
status:
  observedGeneration: -1    # Misleading "never seen" indicator
  conditions: []            # No status information
```

This created confusion for users who interpreted this as the controller not recognizing the resource, when in reality the resource was queued for processing behind other reconciliations.

## Solution Overview

The implementation introduces a **QueueStatusManager** that provides immediate status updates for resources discovered but waiting for processing. Instead of the misleading `observedGeneration: -1`, resources now show:

```yaml
status:
  observedGeneration: 0     # "Discovered but not processed"
  queueMetadata:
    queuedAt: "2025-01-15T10:40:00Z"
    position: 15            # Optional: position in queue
    estimatedProcessingTime: "2025-01-15T10:45:30Z"  # Optional: estimated start time
  conditions:
  - type: Ready
    status: "False"
    reason: "Queued"
    message: "Resource queued for reconciliation (position 15, estimated processing in 5m30s)"
  - type: Reconciling
    status: "False"
    reason: "Pending"
    message: "Waiting for controller capacity"
```

## Key Components

### 1. Enhanced API Schema (`api/v1/kustomization_types.go`)

Added `QueueMetadata` to the `KustomizationStatus`:
```go
// QueueMetadata provides visibility into reconciliation queue state
type QueueMetadata struct {
    QueuedAt                *metav1.Time `json:"queuedAt,omitempty"`
    Position                *int         `json:"position,omitempty"`
    EstimatedProcessingTime *metav1.Time `json:"estimatedProcessingTime,omitempty"`
}
```

### 2. QueueStatusManager (`internal/queue/status_manager.go`)

Core component that:
- Tracks resources in the reconciliation queue
- Updates resource status immediately upon discovery
- Provides position tracking and time estimation (optional features)
- Manages cleanup when resources start processing

Key methods:
- `TrackQueued(ctx, req)` - Called when resource first seen with `observedGeneration: -1`
- `TrackDequeued(req)` - Called when reconciliation begins
- `StartStatusUpdater(ctx)` - Background process for position updates

### 3. Feature Flags (`internal/features/features.go`)

Three feature flags control the behavior:
```go
// Basic queue status (enabled by default)
QueueStatusReporting = "QueueStatusReporting"

// Advanced features (disabled by default, experimental)
QueuePositionTracking = "QueuePositionTracking"
QueueTimeEstimation = "QueueTimeEstimation"
```

### 4. Controller Integration (`internal/controller/kustomization_controller.go`)

Modified `Reconcile` method to:
1. Track newly discovered resources (`observedGeneration: -1`)
2. Mark resources as dequeued when processing begins
3. Clear queue metadata when transitioning from queued to processing

### 5. Manager Setup (`internal/controller/kustomization_manager.go`)

Integration into controller setup with feature flag checks and background updater initialization.

## Usage and Configuration

### Default Behavior (Enabled)

By default, `QueueStatusReporting` is enabled, providing basic queue visibility without performance overhead.

### Advanced Features (Experimental)

Enable additional features via command line flags:
```bash
# Enable position tracking
--feature-gates=QueuePositionTracking=true

# Enable time estimation
--feature-gates=QueueTimeEstimation=true

# Enable both
--feature-gates=QueuePositionTracking=true,QueueTimeEstimation=true
```

## State Transitions

```
[Resource Created] 
    ↓
[observedGeneration: -1] (Undiscovered)
    ↓ (Watch event received)
[observedGeneration: 0, QueueMetadata] (Queued)
    ↓ (Reconcile begins)
[QueueMetadata cleared, Reconciling] (Processing)
    ↓
[observedGeneration: N, Ready/Failed] (Complete)
```

## Performance Considerations

- **Memory**: ~100 bytes per queued resource
- **API Calls**: Batched status updates every 30 seconds
- **Processing**: Zero impact on reconciliation performance
- **Cleanup**: Automatic cleanup prevents memory leaks

## Testing

Unit tests cover:
- Basic queue tracking functionality
- Feature flag behavior (positions, estimation)
- Edge cases (already processed resources, deletions)
- Status update logic and message formatting

Run tests:
```bash
go test ./internal/queue/...
```

## Migration

This is a purely additive change:
- **No breaking changes** to existing APIs
- **Backward compatible** - existing tooling continues to work
- **Immediate benefit** - better UX without configuration changes
- **Optional features** can be enabled as desired

## Benefits

### For Users
- ✅ Clear distinction between "undiscovered" vs "queued" resources
- ✅ Visibility into queue depth and processing time
- ✅ Reduced confusion during high-load scenarios
- ✅ Better capacity planning data

### For Operators
- ✅ Improved troubleshooting with actionable status information
- ✅ Capacity planning data via queue metrics
- ✅ Reduced false incident reports
- ✅ Better understanding of controller performance

### For the Flux Ecosystem
- ✅ Pattern can be extended to other Flux controllers
- ✅ Improved overall GitOps user experience
- ✅ Enhanced observability and operational confidence

## Future Enhancements

- Prometheus metrics for queue depth and processing times
- Advanced queue management (priority queues, partitioning)
- Cross-controller coordination and shared queue visibility
- Integration with Flux monitoring and alerting tools

## Implementation Notes

- Feature-flagged for gradual rollout
- Configurable update intervals for position tracking
- Robust error handling with graceful degradation
- Memory-bounded with automatic cleanup
- Thread-safe concurrent access patterns