# Bug Report: Kustomizations get stuck on old git revisions when dependency checks fail

## Summary

Kustomizations with `dependsOn` can become permanently stuck attempting old git revisions when dependency checks fail, even after dependencies are resolved. This creates dependency chain deadlocks where kustomizations cannot progress to newer git revisions.

## Root Cause

The `LastAttemptedRevision` status field is only updated **after** successful dependency checks (inside the `reconcile()` function), not when the reconciliation attempt begins. If dependency checks fail, the kustomization retries the same old revision indefinitely instead of attempting newer revisions that might resolve the dependency issue.

**Problematic code flow:**
1. Get latest git revision from source artifact 
2. Check dependencies against current source revision
3. **If dependency check fails → return early, never update `LastAttemptedRevision`**
4. **If dependency check passes → call `reconcile()` → update `LastAttemptedRevision`**

## Impact

- Dependency chains become permanently deadlocked
- Manual intervention required to unstick kustomizations
- GitOps workflows fail to progress through git revisions
- Systems cannot recover from transient dependency failures

## Steps to Reproduce

### Prerequisites
- Flux v2.6+ with kustomize-controller
- Two Kustomizations with dependency relationship
- Git repository with multiple commits

### Setup

1. Create two Kustomizations where `child` depends on `parent`:

```yaml
# parent.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: parent
  namespace: flux-system
spec:
  interval: 1m
  sourceRef:
    kind: GitRepository
    name: test-repo
  path: ./parent
  prune: true
```

```yaml
# child.yaml  
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: child
  namespace: flux-system
spec:
  interval: 1m
  sourceRef:
    kind: GitRepository
    name: test-repo
  path: ./child
  prune: true
  dependsOn:
    - name: parent
```

2. Apply both kustomizations to cluster
3. Wait for both to reconcile successfully (both show same `lastAppliedRevision`)

### Reproduction Steps

1. **Create scenario where parent fails temporarily**:
   - Make a git commit that breaks the parent kustomization
   - Push to repository  
   - Parent fails, child shows "dependency revision is not up to date"

2. **Fix parent and create new commit**:
   - Make another git commit that fixes the parent 
   - Push to repository
   - Parent reconciles successfully with newer revision

3. **Observe the bug**:
   ```bash
   kubectl get kustomizations -o custom-columns="NAME:.metadata.name,READY:.status.conditions[?(@.type=='Ready')].status,LAST_ATTEMPTED:.status.lastAttemptedRevision,LAST_APPLIED:.status.lastAppliedRevision"
   ```

### Expected Behavior
- Child should attempt the latest git revision
- Child should see parent has same revision and dependency check should pass
- Child should reconcile successfully

### Actual Behavior  
- Child remains stuck attempting the old git revision (when dependency first failed)
- Dependency check fails because child compares old attempted revision against parent's new applied revision
- Error: `dependency 'parent' revision is not up to date`
- Child never attempts newer git revisions

### Example Output
```
NAME     READY   LAST_ATTEMPTED                           LAST_APPLIED
parent   True    master@sha1:abc123-new-commit           master@sha1:abc123-new-commit  
child    False   master@sha1:def456-old-commit           master@sha1:def456-old-commit
```

Child shows:
```
Status: dependency 'parent' revision is not up to date
```

## Code Analysis

**Problem location:** `internal/controller/kustomization_controller.go` lines ~225-250

```go
// Get latest revision from git source
revision := artifactSource.GetArtifact().Revision  // ← This gets LATEST revision

// Check dependencies 
if len(obj.Spec.DependsOn) > 0 {
    if err := r.checkDependencies(ctx, obj, artifactSource); err != nil {
        // Return early without updating LastAttemptedRevision ← BUG: stuck on old revision
        return ctrl.Result{RequeueAfter: r.requeueDependency}, nil  
    }
}

// Only reached if dependencies pass
reconcileErr := r.reconcile(ctx, obj, artifactSource, patcher, statusReaders)
```

**Inside `reconcile()` function (line ~345):**
```go  
// This only happens AFTER dependency checks pass
obj.Status.LastAttemptedRevision = revision  // ← BUG: too late
```

## Proposed Fix

Move `LastAttemptedRevision` update to happen **before** dependency checks:

```go
revision := artifactSource.GetArtifact().Revision
originRevision := getOriginRevision(artifactSource)

// Update the last attempted revision before dependency checks to ensure
// that we don't get stuck retrying the same old revision indefinitely  
// when dependencies are updated to newer revisions.
obj.Status.LastAttemptedRevision = revision
if err := r.patch(ctx, obj, patcher); err != nil {
    return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
}

// Now check dependencies with current revision properly tracked
if len(obj.Spec.DependsOn) > 0 {
    if err := r.checkDependencies(ctx, obj, artifactSource); err != nil {
        // Now when this fails, we've already recorded attempting the latest revision
        return ctrl.Result{RequeueAfter: r.requeueDependency}, nil
    }
}
```

**Also remove duplicate assignment in `reconcile()` function (line ~350):**
```go
// Remove this line since LastAttemptedRevision already set earlier
// obj.Status.LastAttemptedRevision = revision  ← DELETE
```

## Test Results

We implemented and tested this fix in production:

**Before fix:**
```
NAME             READY   LAST_ATTEMPTED                      LAST_APPLIED
trust-manager    True    master@sha1:1f4e62fc (new)        master@sha1:1f4e62fc  
ca-bundles       False   master@sha1:67db00bc (old)        master@sha1:67db00bc
```

**After fix:**
```  
NAME             READY   LAST_ATTEMPTED                      LAST_APPLIED
trust-manager    True    master@sha1:ce2ae11e (latest)      master@sha1:ce2ae11e
ca-bundles       True    master@sha1:ce2ae11e (latest)      master@sha1:ce2ae11e  
```

The dependency chain deadlock was completely resolved.

## Environment

- **kustomize-controller version**: v1.6.1
- **Flux version**: v2.6.4
- **Kubernetes version**: v1.31+
- **Reproduced on**: Production cluster with complex dependency chains

## Additional Context

This bug particularly affects:
- Complex GitOps setups with multiple dependency layers  
- Infrastructure bootstrap sequences (cert-manager → ca-issuers → applications)
- Any scenario where dependencies temporarily fail during git revision changes

The bug can cause complete GitOps workflow failures that require manual intervention to resolve.

## Workaround

Temporary workarounds until fix is available:
1. Remove `dependsOn` relationships temporarily
2. Manually delete and recreate stuck kustomizations  
3. Suspend/resume stuck kustomizations to force revision refresh