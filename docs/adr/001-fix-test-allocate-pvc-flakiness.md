# ADR 001: Fix TestAllocateWithPVC Flakiness

**Status:** Accepted  
**Date:** 2026-01-16  
**Authors:** hajnalmt  
**Related Issue:** [#4850](https://github.com/volcano-sh/volcano/issues/4850)  
**Related PR:** [#4858](https://github.com/volcano-sh/volcano/pull/4858) (original attempt)

---

## Context

The unit test `TestAllocateWithPVC` in `pkg/scheduler/actions/allocate/allocate_test.go` fails intermittently in CI with the error:

```
case 1(static pv matched and node with enough resources) check podgroup <c1/pg1> 
task status Binding: want 2, got 0
```

### Root Cause Analysis

The test failure is caused by a **race condition between scheduler actions and informer cache synchronization**:

1. **Test Setup:** Creates storage resources (PVs, PVCs, StorageClass) using fake Kubernetes client
2. **Storage Class Mode:** Uses `VolumeBindingMode: WaitForFirstConsumer`
3. **Complex Interaction Required:**
   - Scheduler must decide pod placement
   - Volume binding plugin binds PV to PVC
   - Pod status transitions to Binding
4. **The Problem:** In test environment with fake clients, informer cache sync is **not instantaneous**
5. **Result:** On slower CI runners, test assertions execute **before** the cache reflects volume binding operations
6. **Outcome:** Test sees 0 pods in Binding status when expecting 2

### Previous Fix Attempt

PR #4858 attempted to fix this but had issues:
- **Scope creep:** 268 additions, 68 deletions across 13 files
- **Unrelated changes:** network-qos tests, agent tests, TDM plugin modifications
- **Reviewer feedback:** @hzxuzhonghu and @JesseStutler requested "smallest change" and removal of "unrelated code change"
- **Still had races:** Even with changes, other race conditions were reported

---

## Decision

We will implement **Option 1: Add Explicit Cache Sync Wait in Test Helper** using `PollUntilContextTimeout` for polling.

### Why This Approach?

1. **Addresses root cause properly:** Explicitly waits for informer cache sync rather than hoping a fixed sleep is enough
2. **Follows Kubernetes patterns:** Uses `cache.WaitForCacheSync` and `wait.PollUntilContextTimeout` (standard approaches)
3. **Minimal and focused:** < 50 total lines changed across 2 files
4. **Reusable:** Pattern can be applied to other storage-related tests
5. **Configurable:** Tests can override timeout if needed
6. **Follows Volcano patterns:** Identical to existing usage in `binder_test.go:344`
7. **Non-deprecated:** Uses modern context-aware polling (not deprecated `PollImmediate`)

---

## Alternatives Considered

### Option 2: Add Polling Retry in Test Assertion
**Approach:** Modify `CheckTaskStatusNums` to retry with exponential backoff

**Rejected because:**
- Hides timing issues rather than addressing them
- Could mask real bugs
- Doesn't follow "fail fast" testing principle
- Less explicit about the wait

### Option 3: Simple Sleep After Resource Creation
**Approach:** Add `time.Sleep(3 * time.Second)` after creating storage resources

**Rejected because:**
- Fixed timeout (not adaptable)
- Always waits full duration (can't short-circuit)
- Less sophisticated than polling
- May still fail on extremely slow runners

---

## Implementation Details

### Technical Decision: `PollUntilContextTimeout` vs `PollImmediate`

**Choice:** `wait.PollUntilContextTimeout` with `immediate=true`

**Reasons:**
1. `PollImmediate` is **deprecated** (will be removed in future release)
2. Volcano codebase already uses `PollUntilContextTimeout` in `binder_test.go`
3. Better context handling and cancellation support
4. Returns context-aware errors
5. The `immediate=true` parameter checks condition immediately without initial wait

### Two-Phase Cache Sync Strategy

The implementation uses a two-phase approach:

**Phase 1:** `cache.WaitForCacheSync` - Ensures informer infrastructure is ready
```go
cache.WaitForCacheSync(ctx.Done(),
    sc.informerFactory.Core().V1().PersistentVolumes().Informer().HasSynced,
    sc.informerFactory.Core().V1().PersistentVolumeClaims().Informer().HasSynced,
    sc.informerFactory.Storage().V1().StorageClasses().Informer().HasSynced)
```

**Phase 2:** `wait.PollUntilContextTimeout` - Verifies specific test resources are visible in cache
```go
wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, 
    func(ctx context.Context) (bool, error) {
        // Check each PV and PVC exists in lister cache
    })
```

This approach:
- ✅ Fast when cache is already synced (`immediate=true` means no unnecessary wait)
- ✅ Robust on slow runners (polls up to timeout if cache sync is slow)
- ✅ Distinguishes "informer not ready" from "resources not synced"

---

## Changes Required

### File 1: `pkg/scheduler/uthelper/helper.go`

#### 1. Add field to TestCommonStruct (around line 100)
```go
type TestCommonStruct struct {
    // ... existing fields ...
    
    // CacheSyncTimeout specifies how long to wait for storage resource cache sync.
    // If zero, defaults to 3 seconds. Only applies when PVs, PVCs, or SCs are present.
    CacheSyncTimeout time.Duration
    
    // ... rest of fields ...
}
```

#### 2. Modify createSchedulerCache (after line 160)
```go
// Wait for storage resource cache synchronization
if len(test.PVs) > 0 || len(test.PVCs) > 0 || len(test.SCs) > 0 {
    timeout := test.CacheSyncTimeout
    if timeout == 0 {
        timeout = 3 * time.Second
    }
    
    if err := test.waitForStorageCacheSync(schedulerCache, timeout); err != nil {
        klog.V(2).InfoS("Storage cache sync incomplete", "timeout", timeout, "err", err)
    }
}
```

#### 3. Add new helper method (at end of file)
```go
// waitForStorageCacheSync waits for storage-related informers to sync and verifies
// that test resources are visible in the cache. This prevents race conditions in tests
// involving PVs, PVCs, and StorageClasses.
func (test *TestCommonStruct) waitForStorageCacheSync(sc *cache.SchedulerCache, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    // Phase 1: Wait for informers to complete initial sync
    if !cache.WaitForCacheSync(ctx.Done(),
        sc.informerFactory.Core().V1().PersistentVolumes().Informer().HasSynced,
        sc.informerFactory.Core().V1().PersistentVolumeClaims().Informer().HasSynced,
        sc.informerFactory.Storage().V1().StorageClasses().Informer().HasSynced) {
        return fmt.Errorf("informer cache sync timeout after %v", timeout)
    }
    
    // Phase 2: Poll to verify our specific test resources are visible in cache
    return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true,
        func(ctx context.Context) (bool, error) {
            pvLister := sc.informerFactory.Core().V1().PersistentVolumes().Lister()
            pvcLister := sc.informerFactory.Core().V1().PersistentVolumeClaims().Lister()
            
            // Check all PVs are in cache
            for _, pv := range test.PVs {
                if _, err := pvLister.Get(pv.Name); err != nil {
                    klog.V(4).InfoS("Waiting for PV in cache", "pv", pv.Name)
                    return false, nil // Keep polling
                }
            }
            
            // Check all PVCs are in cache
            for _, pvc := range test.PVCs {
                if _, err := pvcLister.PersistentVolumeClaims(pvc.Namespace).Get(pvc.Name); err != nil {
                    klog.V(4).InfoS("Waiting for PVC in cache", "pvc", pvc.Namespace+"/"+pvc.Name)
                    return false, nil // Keep polling
                }
            }
            
            return true, nil // All resources found
        })
}
```

### File 2: `pkg/scheduler/actions/allocate/allocate_test.go`

#### Modify test loop (around line 2644)
```go
for i, test := range tests {
    t.Run(test.Name, func(t *testing.T) {
        predicates.ResetVolumeBindingPluginForTest()
        test.Plugins = plugins
        test.CacheSyncTimeout = 5 * time.Second  // Explicit timeout for PVC tests
        test.RegisterSession(tiers, nil)
        defer test.Close()
        action := New()
        test.Run([]framework.Action{action})
        if err := test.CheckAll(i); err != nil {
            t.Fatal(err)
        }
    })
}
```

---

## Impact Analysis

### Performance Impact
- **Test Duration:** Adds 0-5 seconds per test (typically <1s on fast machines due to `immediate=true`)
- **CI Impact:** Negligible - timeout only reached on slow runners where test would have failed anyway
- **Fast Path:** When cache is already synced, the poll returns immediately

### Code Complexity
- **Lines Added:** ~50 lines (1 field, 1 method, 1 call, 1 test modification)
- **Lines Removed:** 0
- **Files Modified:** 2
- **Maintainability:** High - clear intent, well-commented, follows existing patterns

### Reusability
The `CacheSyncTimeout` field and `waitForStorageCacheSync` method can be used by:
- Other allocate tests involving storage
- Preempt/reclaim tests with PVCs
- Any scheduler action test using `WaitForFirstConsumer` volumes

---

## Verification Plan

1. **Run test locally 50+ times:** `go test -run TestAllocateWithPVC -count=50`
2. **Run with race detector:** `go test -race -run TestAllocateWithPVC`
3. **Verify CI passes:** Push to PR and check GitHub Actions
4. **Check test duration:** Ensure timeout doesn't significantly impact CI time

---

## Success Criteria

- ✅ Test passes consistently 100+ times locally
- ✅ Test passes in CI without flakiness
- ✅ No race conditions detected
- ✅ Test duration remains reasonable (<10s including setup)
- ✅ Code review approval from maintainers
- ✅ Pattern is reusable for other storage tests

---

## References

- **Issue:** https://github.com/volcano-sh/volcano/issues/4850
- **Original PR attempt:** https://github.com/volcano-sh/volcano/pull/4858
- **Kubernetes wait package:** `k8s.io/apimachinery/pkg/util/wait`
- **Similar pattern in codebase:** `pkg/scheduler/capabilities/volumebinding/binder_test.go:344`
- **Context-aware polling:** https://pkg.go.dev/k8s.io/apimachinery/pkg/util/wait#PollUntilContextTimeout

---

## Notes

- This fix focuses **only** on the cache sync timing issue
- Does not address other potential races in the volume binding plugin itself (separate concern)
- The `WaitForFirstConsumer` binding mode is inherently complex and may have other edge cases
- Test comment (line 2616-2618) acknowledges complexity of mocking pv-controller

---

## Implementation Status: INCOMPLETE - Further Investigation Required

**Date:** 2026-01-16

After attempting implementation, we discovered the issue is more complex than initially analyzed:

### What We Learned

1. **Nodes must be in informer cache** - Volume binding PreBind requires nodes to be accessible via informer, not just scheduler cache
2. **Pods must be in informer cache** - PreBind also needs pods accessible via API client
3. **Dual resource creation paths** - Storage tests need resources created via fake API (for informers), non-storage tests use direct cache adds
4. **Asynchronous binding** - Volume binding happens **after** session closes, but test checks session state

### Key Discovery

The test expects `ExpectTaskStatusNums: {api.Binding: 2}` but pods never reach Binding status because:
- Allocate action runs during session
- Session is checked immediately after action completes
- Volume binding PreBind happens asynchronously via cache workers
- Task status updates to Binding happen **after** the test assertion runs

### Architecture Issue

The test framework's `TestCommonStruct` has two modes:
1. **Direct cache add** (fast, synchronous) - used by most tests
2. **API + informer** (slow, asynchronous) - needed for volume binding

Mixing these creates timing complexities that simple sleeps/waits cannot reliably solve.

### Attempted Solutions

1. ✅ Cache sync wait - ensures resources in informer (works)
2. ✅ Node/Pod API creation - prevents "not found" errors (works)
3. ❌ Post-action sleep - pods still don't reach Binding state (1 second insufficient)
4. ❌ Increased timeouts - test still fails consistently

### Root Cause Hypothesis

The test expectation may be **incorrect**. The comment on lines 2616-2618 states:
```
// This test case may have error logs, mainly because of the binding PV and PVC depends on pv-controller.
// The mock pv-controller in the UT is too complex and requires accurate timing to trigger the binding of PV and PVC,
// so here the UT only verifies the status of podgroup
```

This suggests the test **should** only check `ExpectStatus` (PodGroup status), not `ExpectTaskStatusNums` (task Binding status).

---

## Recommended Next Steps

1. **Investigate test history** - When was `ExpectTaskStatusNums: {api.Binding: 2}` added? Was it ever passing reliably?
2. **Consider alternative assertion** - Change test to only verify PodGroup status (as comment suggests)
3. **Mock pv-controller properly** - Implement proper fake PV controller that triggers binding
4. **Redesign test architecture** - Create separate test helper for storage tests that handles async binding

---

## Decision Log

- **2026-01-16:** Initial analysis and option comparison
- **2026-01-16:** Decided on `PollUntilContextTimeout` over deprecated `PollImmediate`
- **2026-01-16:** Chose two-phase sync approach (HasSynced + resource polling)
- **2026-01-16:** Set default timeout to 3 seconds, test-specific override to 5 seconds
- **2026-01-16:** Discovered nodes/pods must be in informer cache for PreBind
- **2026-01-16:** Implemented dual-path resource creation (API for storage tests, direct for others)
- **2026-01-16:** **STATUS: INCOMPLETE** - Test expectations may be incorrect, requires further investigation
