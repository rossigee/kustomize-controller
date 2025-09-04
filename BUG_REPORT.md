# Bug Report: Queue Visibility Enhancement - Improve Resource Status During High Load

## Problem Summary

During high load conditions, the current Kustomization controller status reporting can create confusion where resources appear "invisible" to users when they are actually queued for processing behind failing reconciliations.

## Current Problematic Behavior

When new Kustomization resources are created during controller saturation (high failure load), they show:

```yaml
status:
  observedGeneration: -1    # Suggests "never seen by controller"
  conditions: []            # No status information at all
```

**User Experience**: "The controller doesn't know this resource exists" ❌  
**Reality**: Resource discovered and queued, but waiting behind failed reconciliations

## Root Cause Analysis

### Queue Saturation Under High Failure Load

1. **Controller Discovery**: Resources are immediately discovered via watch events
2. **Queue Placement**: New reconciliation requests added to controller's work queue  
3. **Processing Delay**: Queued behind repeatedly failing reconciliations that consume processing time
4. **Status Gap**: No status updates until first reconciliation attempt completes
5. **Misleading Signal**: `observedGeneration: -1` suggests controller ignorance rather than queue backlog

### Evidence

- **Restart "Fix"**: Restarting controller appears to fix the issue because it clears the queue and forces immediate processing
- **Watch Events Working**: Controller receives watch events immediately (logs show discovery)
- **Queue Inspection**: Under high load, work queue contains hundreds of pending items
- **Classic Pattern**: This is a well-known queue saturation problem in high-throughput systems

## Impact Assessment

### User Experience Challenges
- ❌ **Unclear Status Signals**: Users may interpret `observedGeneration: -1` as controller issues when it's actually queue backlog
- ❌ **Diagnosis Complexity**: Limited information available to distinguish between undiscovered vs queued resources
- ❌ **Operational Overhead**: May lead to unnecessary controller restarts and troubleshooting effort
- ❌ **Capacity Visibility Gap**: No clear visibility into controller capacity or queue depth
- ❌ **Monitoring Challenges**: Difficult to distinguish between actual problems and normal queue processing

### Operational Impact
- ⚠️ **Capacity Planning**: Limited data available for making informed scaling decisions
- ⚠️ **Incident Response**: Additional effort required to distinguish queue saturation from actual issues
- ⚠️ **User Experience**: May impact user confidence during high-load scenarios  
- ⚠️ **Knowledge Requirements**: Requires understanding of internal queue mechanics for effective troubleshooting

## Current Code Analysis

### Default Status Initialization
```go
// api/v1/kustomization_types.go:381
// +kubebuilder:default:={"observedGeneration":-1}
Status KustomizationStatus `json:"status,omitempty"`
```

### Controller Reconciliation Entry Point
```go
// internal/controller/kustomization_controller.go:114
func (r *KustomizationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
    // ... fetches resource from API server
    // ... only sets status after first successful/failed processing attempt
    obj.Status.ObservedGeneration = obj.Generation  // Line 237/1166
}
```

### Queue Management
- Uses standard controller-runtime workqueue with rate limiting
- No queue position tracking or status reporting
- No differentiation between "undiscovered" vs "queued" resources

## Reproduction Scenario

1. **Setup**: Deploy multiple Kustomizations with some failing reconciliations
2. **Load Generation**: Create additional failing resources to saturate queue
3. **Test Resource**: Create new Kustomization resource  
4. **Observation**: New resource shows `observedGeneration: -1` indefinitely
5. **Restart**: Restart controller → immediate processing and proper status

## Frequency & Severity

- **Frequency**: Common in production environments with:
  - Multiple teams deploying simultaneously
  - Transient infrastructure issues causing reconciliation failures
  - Large numbers of resources with complex dependencies
  
- **Severity**: Moderate to high impact on user experience and operational efficiency
  - May lead to increased troubleshooting overhead
  - Can result in unnecessary controller restarts
  - Impacts the clarity of system status during high-load periods

## Proposed Solution Preview

Implement queue-aware status reporting that distinguishes between:
- **Undiscovered** (`observedGeneration: -1`) - Controller hasn't seen the resource
- **Queued** (`observedGeneration: 0`, `Ready=False/Queued`) - Discovered but waiting for processing
- **Processing** (`observedGeneration: N`, `Ready=False/Reconciling`) - Currently being reconciled

Details in accompanying enhancement proposal.

---

**Environment**: 
- kustomize-controller: v1.4.0+ (affects all recent versions)
- Kubernetes: v1.28+ (controller-runtime behavior)
- Flux: v2.0+ (architectural pattern applies to entire Flux ecosystem)

**Reproducibility**: Consistently reproducible under load conditions
**Workaround**: Restart controller (clears queue) - not suitable for production
**Business Impact**: Moderate - affects user experience and operational efficiency