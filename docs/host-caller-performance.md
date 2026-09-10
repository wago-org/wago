# Concrete synchronous caller: invariants and measurements

This follow-up to [the first pass](host-roundtrip-performance.md) keeps the
native scheduler protocol unchanged.

## Invariants before implementation

- A caller is a value with private authority and an immutable generation
  snapshot. Copies have the same expiry. Reusing dispatch storage cannot renew
  a retained caller. Nested callbacks restore the outer active generation, but
  never restore the monotonic sequence. Exhaustion remains an error.
- Both public caller representations resolve to the same private token and use
  the same owner, instance, invocation, reservation, signature and scope checks.
  A zero caller has no authority. No host input supplies runtime identity.
- The public root identity and callee identity remain distinct across native
  transfers. Lease ownership is determined by the runtime, not the callback
  signature.
- GC suspension may be omitted only when the actual lease owner has no local,
  imported or dynamic collector-domain requirement. Root publication, execution
  leases, result checks, interruption and all resume cleanup stay in place.
- Atomic publication and callback-generation observations retain their current
  synchronization. An activation-local cache must not outlive its native loop
  or replace an outer activation during re-entry.
- The legacy argument/result buffer contract is unchanged. Direct native-frame
  views and bounded scheduler segments are separate experiments, not implied
  by a concrete callback type.

Each production step records its own measurements below. Allocations from host
code itself are not runtime allocation savings.

## Concrete API

```go
imports := wago.Imports{
    "env.step": wago.CallerHostFunc(func(caller wago.Caller, p, r []uint64) {
        r[0] = p[0] + 1
    }),
}
```

`ImportModuleBuilder.CallerFunc` is the plugin equivalent of `Func`. It uses
the existing plugin call gate, admission reservation and exact GC signature
rules. The legacy `HostFunc` and owned `HostFuncRef` APIs are unchanged.
`Caller` implements the existing optional host-module interfaces, including
guest storage, externrefs and GC result construction. Resolver, invoker,
invocation-context and manager methods accept the concrete value through their
existing `HostModule` parameter. Calling such helpers can have its own cost;
the zero-allocation claim applies to direct scalar dispatch with a no-op host.

The caller contains the same 64-byte private token as the legacy path. It is
never pooled. Each synchronous binding adds one function pointer (8 bytes on
the measured target); the per-instance activation sidecar is unchanged.
TinyGo represents each function value with 16 bytes, so its binding grows from
32 to 48 bytes. The footprint test computes the exact compact layout from the
two function-value sizes, descriptor pointer and index/flag; it does not accept
an enlarged standard-Go layout just because TinyGo needs more space.
The updated footprint assertion passes with Go. Local TinyGo linking stops at
the checkout's existing duplicate `tinygo_task_exit` symbol, before tests run;
the native CI TinyGo jobs are used to verify the 48-byte layout.
Reference dispatch keeps argument translation, exact result validation and
temporary root/token cleanup. Imported starts and re-exports also dispatch the
concrete value directly. Raw callbacks do not gain plugin GC import authority.

Escape analysis distinguishes leaking the token's pointer fields (permitted,
because host code may retain the token) from allocating storage for a boxed
token. The legacy `b.fn(caller, ...)` branch boxes 64 bytes. The
`b.concrete(Caller{...}, ...)` branch passes the value without that box.
`TestCallerScalarDispatchAllocations` checks zero allocations after warm-up.

## Fresh baseline

Linux/amd64, Go 1.27.1, Ryzen 7 8845HS, GOMAXPROCS=16. Samples use 500 ms,
five repetitions, with no other Wago builds running during measurement.
Other desktop workloads are not under this task's control; report ranges and
do not compare against the earlier report as a fresh baseline.

The public one-call median is 416.4 ns (413.1–430.0), 64 B and one allocation.
The 1,024-call loop median is 233,111 ns (226,133–234,994), 65,536 B and
1,024 allocations per public invocation. The matched guest loop median is
604.2 ns. Their difference is approximately 227.1 ns per repeated host call.
Parallel independent instances take a median 37,027 ns per 1,024-call public
invocation (36,767–38,002), before subtracting their matched guest control.

## Portable fixture validation

Commit `1b67194a4` passed the full CI matrix, including Darwin/amd64,
Windows/amd64 and Windows/arm64. The local package suite passed in 6.278 s.
The four-memory case is additional coverage only; both basic host-admission
assertions still run on every supported platform.

## Concrete caller checkpoint

Five 500 ms samples; median (range). Loop values below are public invocations
with 1,024 callbacks, except the first two rows.

| Path | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Legacy public single | 381.6 (378.8–391.9) | 64 | 1 |
| Concrete public single | 348.3 (345.5–348.8) | 0 | 0 |
| Legacy memory-0 loop | 188967 (186085–191807) | 65536 | 1024 |
| Concrete memory-0 loop | 164008 (161270–165352) | 0 | 0 |
| Legacy parallel loop | 36987 (36496–37797) | 65538 | 1024 |
| Concrete parallel loop | 19621 (19571–19924) | 0 | 0 |
| Legacy local GC loop | 272766 (267895–273262) | 65536 | 1024 |
| Concrete local GC loop | 242402 (239860–247808) | 0 | 0 |
| Concrete imported domain | 184636 (182241–185508) | 0 | 0 |
| Concrete dynamic domain | 196096 (194592–197124) | 0 | 0 |

Matched guest controls give 184.0 ns/call legacy and 159.6 ns/call concrete.
The parallel differences are 36.0 and 19.1 ns/call in aggregate (about 27.8
and 52.4 million callbacks/s). Earlier baseline legacy samples were slower;
the same-checkpoint legacy/concrete comparison better isolates boxing removal.

The 10-second concrete memory-0 profile attributes 14.06% cumulative / 13.40%
flat CPU to `suspendGCInvocation`, 9.44% cumulative to scope setup, and 11.05%
flat to unresolved external code. These percentages overlap where cumulative.
The allocation profile has no sampled callback boxing; its samples are test,
compiler and profiler setup. The mutex profile records only about 481 us of
runtime/profiler delay, not a Wago global bookkeeping bottleneck.

The full package suite passed after the API change (5.898 s), as did the full
race suite (256.781 s with the default per-process exit delay). A second full
race run including the added helper/start/re-export tests passed in 20.733 s
with `GORACE=atexit_sleep_ms=0`.

### Scope and buffer review

Go atomics are sequentially consistent. This pass does not weaken them. The
active generation is read by retained tokens and watchers on other goroutines;
the optional state uses atomic publication. The sequence must never roll back
on nested return and must remain saturated after exhaustion. Compiler output
currently rejects inlining `nextGeneration` (cost 123), `beginReservedWithID`
(94), `beginHostCallScopeReservedWithID` (123), and `end` (129); `valid` inlines
(28). Any scope change must be measured, not inferred from these costs alone.

Small-arity buffers remain in the Go engine scratch area. Nested re-entry uses
an isolated engine/control frame, with the runtime's scratch-in-use fallback
kept intact. Exposing native control-frame slices would give a retained slice
an off-heap lifetime that caller-generation checks cannot revoke. Exact slice
capacity and documented borrowing would help, but do not themselves prevent
use after native storage is released. Reference translation also writes public
tokens into arguments and cannot be moved onto native root storage casually.
The arity benchmark measures the existing copy path. No direct-frame path is
enabled and no direct-frame speedup is claimed.

Segment scheduler admission also stays disabled. A callee's bounded prefix says
nothing about an unbounded caller continuation. Repeated bounded/host segments
need explicit scheduler-progress boundaries across the whole native stack.
Arbitrary Go callbacks still execute through the normal Go scheduler protocol.

## No-GC suspension proof

The predicate reads the actual `leaseOwner`, after parked roots and arguments
are published and before native ownership is released. It admits omission only
when `gc == nil` and none of ImportedGCDomain, DynamicGCDomain or
StoreOwnedGCCollector is set. Registration establishes these flags before
invocation; dynamic table/global reachability gets the dynamic flag even when
the domain topology is currently empty. `gcInvocationDomains` already returns
an empty view for the admitted case. The generic routine's separate dynamic
topology work is also impossible in that case.

GC-capable calls create the existing suspension conditionally on the original
Go dispatch frame; escape analysis keeps the value itself on the stack. Both
branches use the same parked-callback cleanup. It restores collector leases, if any,
before native ownership, then restores context and roots in the original
order. The no-domain path neither constructs the large suspension value nor
calls its generic suspend/resume functions. No collector lock is removed when
a collector lease exists. Root identity selection is unchanged, including a
non-GC public relay that owns multiple producer domains.

Tests include admission metadata, both caller APIs against forced general
cleanup, and both APIs in a two-domain scalar relay whose host callback collects
and re-enters the other producer. Existing dynamic-domain installation tests
remain in the full suite. No new cache, counter or growing map is introduced.

An initial split-helper layout was rejected: it improved private repeated calls
but increased the concrete local-GC loop from 242,402 to 285,298 ns per public
invocation. The conditional stack-local value keeps the original call structure.
Disassembly confirms that the no-domain branch skips the suspension call and
its 80-byte result copy; it only passes a nil pointer to the shared cleanup.

The conditional-value checkpoint measures 139.3 ns per repeated concrete call
(143,226 ns per 1,024-call invocation; range 142,623–145,968), 0 B and 0
allocations. The matching legacy loop is 175,644 ns (172,533–175,950), or
171.0 ns/call after guest subtraction. Concrete parallel calls measure 17.9
ns/call in aggregate. Public single-call medians are 345.8 ns concrete and
381.6 ns legacy.

To check the fallback separately, a reference binary was built from
`05a8f0b89`, then both binaries ran ten 2-second samples, without concurrent
builds. Medians (ranges), ns per 1,024-call public invocation:

| Concrete fallback | Reference | Conditional value | Delta |
|---|---:|---:|---:|
| Local GC | 248261.5 (246056–256299) | 251565.5 (250506–252532) | +1.3% |
| Imported domain | 191283 (190569–197059) | 204412.5 (200729–207536) | +6.9% |
| Dynamic domain | 221451.5 (213262–225136) | 217743.5 (213739–222029) | -1.7% |

This checkpoint is a private-path win with a measured imported-domain tradeoff,
not an across-the-board improvement. All concrete fallback samples remain at
0 B/op and 0 allocs/op. The next scope step must check these paths again.
The no-GC CPU profile has no `suspendGCInvocation` samples. Scope setup now
accounts for 11.67% cumulative CPU, which supports measuring sidecar reuse next.
The revised full package suite passed in 5.811 s; its full race run passed in
20.663 s. Generation, cancellation, root and scheduler tests remain enabled.

## Scope-address reuse checkpoint

The once-created dispatcher captures the address of the atomically published
host scope. That sidecar is never replaced. This removes the repeated
`ensurePluginState` lookup and the intermediate token return through
`beginHostCallScopeReservedWithID`. It does not cache identity or generation.
Both scalar and reference dispatch call the same existing scope constructor.
All atomic operations and lazy optional watcher/context state remain unchanged.
The dispatcher closure adds one pointer (8 bytes on the measured target) once
per instance; no unbounded state is added.

Five 500 ms samples, medians (ranges), ns per 1,024-call public invocation:

| Path | Before | Scope reuse |
|---|---:|---:|
| Concrete memory-0 | 143226 (142623–145968) | 128488 (127243–129618) |
| Legacy memory-0 | 175644 (172533–175950) | 150659 (149333–152323) |
| Concrete parallel | 18419 (18317–19411) | 17939 (17759–18266) |
| Concrete local GC | 254001 (251212–255975) | 248816 (245143–252162) |
| Concrete imported domain | 201729 (200729–202151) | 202161 (199451–204414) |
| Concrete dynamic domain | 220598 (215120–222400) | 213618 (211758–215346) |

Guest subtraction gives 124.9 ns/call concrete and 146.6 ns/call legacy.
Concrete calls remain 0 B/op and 0 allocs/op in all rows; legacy calls remain
64 B and one allocation per callback. Public single-call medians are 340.3 ns
concrete (338.7–340.8) and 375.0 ns legacy (373.4–393.2).

The full package and race suites passed (5.827 s and 21.452 s). The cross-instance
chain test also checks that each dispatcher uses its own instance's scope.
The profile no longer contains `beginHostCallScopeReservedWithID` on the hot
path. Remaining `beginReservedWithID` is 6.60% cumulative; context-map lookup
is 6.52% cumulative and reservation resolution is 3.00%. These are separate
observations, not additive exclusive CPU percentages.

## Inline reservation lookup

The lookup now checks the inline entry's exact invocation ID before looking in
the fallback map. It retains the mutex, nil-reservation condition, fallback
keys and all swap/restore behavior. No authority is inferred from an occupied
inline slot belonging to a different invocation.

Ten 2-second samples, ns/op; all cases allocate 0 B and 0 objects:

| Lookup | Before median (range) | After median (range) |
|---|---:|---:|
| No reservation | 5.125 (5.085–5.329) | 5.022 (4.902–5.053) |
| Inline | 5.263 (5.238–5.340) | 3.606 (3.597–3.621) |
| Fallback | 6.133 (6.084–6.241) | 5.905 (5.768–5.941) |
| Inline with fallback map | 6.355 (6.341–6.392) | 3.718 (3.702–3.735) |
| Nested swap/lookup/restore | 12.825 (12.800–12.890) | 11.470 (11.440–11.490) |

The benefit is a few nanoseconds for an actual inline reservation, not a large
claim for the no-reservation host loop. Existing nested-identity and cleanup
tests remain unchanged; each benchmark also checks the returned reservation.

The full-loop checkpoint does **not** show a host-call win: concrete memory-0
is 135,821 ns (135,192–136,408), or 132.1 ns/call after guest subtraction;
legacy is 154,570 ns (151,824–160,640). The guest-only control also moved from
556.1 to 579.5 ns. Concrete parallel is 19,210 ns (18,993–19,291), local GC
252,413 ns (252,064–253,805), imported domain 205,573 ns (203,868–206,113), and
dynamic domain 214,547 ns (212,825–215,621). These macro samples do not isolate
the lookup's small benefit, so no large host-call gain is attributed to it.
Public single-call medians are 343.5 ns concrete and 387.8 ns legacy. Allocation
counts are unchanged. The full package and race suites passed in 5.902 s and
20.552 s. The profile still shows context-map lookup (8.13% cumulative) and
reservation lookup (2.61% cumulative), supporting an activation-local cache
experiment next.

## Activation-local context proof, before implementation

The public invocation gate fixes identity and reservation for the native call.
`callNativeSyncWithTrapContext` installs the callback parent before entering its
Go host loop and restores it only when that loop ends. Re-entry installs a
distinct control frame, engine and Go loop, then restores the outer frame and
parent binding. A cache can therefore belong to that Go loop, never to the
instance. It will be initialized only on the first arbitrary host callback, so
zero-host invocations do not pay an identity lookup.

Admission requires a nonzero root identity and the same root control frame as
at entry. A changed control frame uses the existing lookup without replacing
the outer snapshot. A missing root identity uses the existing callee fallback
and is never cached as a root. The cache carries only runtime-derived root ID,
reservation and parent context; GC lease owner and flags remain resolved on
every callback. The value is accessed only by its Go loop's callback, so no
cross-goroutine atomics are removed. Each nested native entry has its own value.
All callback generations, leases, roots, traps and scheduler transitions remain
outside this cache and retain their existing protocol.

The implementation follows this proof with a 48-byte Go-local activation on
amd64. Escape analysis reports that its dispatch method value does not escape.
The cancellation context is a reference to the live parent, not a snapshot of
its cancellation state. A parent cancellation therefore remains observable.
Focused tests cover nested frames, restoration, later invocations that reuse a
frame, and a callee fallback that must never become cached root authority.
The full package and race suites passed in 6.447 s and 22.008 s.

### Context-cache checkpoint

Five 500 ms samples, median (range), ns per 1,024-call public invocation:

| Path | Before | Activation-local cache |
|---|---:|---:|
| Concrete memory-0 | 135821 (135192–136408) | 119013 (118744–119725) |
| Legacy memory-0 | 154570 (151824–160640) | 147718 (144329–153382) |
| Concrete parallel | 19210 (18993–19291) | 18170 (17850–18848) |
| Concrete local GC | 252413 (252064–253805) | 228134 (227282–233132) |
| Concrete imported domain | 205573 (203868–206113) | 181821 (181192–182810) |
| Concrete dynamic domain | 214547 (212825–215621) | 188494 (187490–193866) |

Guest subtraction gives 115.6 ns/call concrete and 143.7 ns/call legacy. All
concrete rows retain 0 B/op and 0 allocs/op. There is a small-call tradeoff:
public single-call latency is 351.6 ns concrete (349.5–354.2), up from 343.5 ns,
and 392.3 ns legacy (390.0–395.9), up from 387.8 ns. Concrete zero-host fixed
invocation cost is 135.2 ns (134.8–136.3). The cache is justified for repeated
calls, not claimed as a single-call improvement.

The profile has no repeated context-map, reservation-lookup or generic GC
suspension samples. The cache helper is 1.58% flat; scalar dispatch is 9.42%
flat / 17.03% cumulative, and native resume is 3.69% flat / 17.18% cumulative.
The allocation profile again samples setup/profiling, not callback boxes.
Mutex delay is 492 us total, attributed to the Go runtime, not global Wago
activation bookkeeping.

## Security and DoS review of this pass

| Change | Removed work and proof | Abuse, protection and fallback |
|---|---|---|
| Concrete caller | Interface box removed only from direct concrete dispatch; the private token is unchanged | Retention cannot change its generation snapshot. All helper APIs resolve the same private authority. Nested return restores active generation, not sequence. Overflow still stops issuance. Legacy dispatch remains available. |
| No-GC suspension | Generic suspension omitted only when the actual lease owner's collector and domain metadata exclude a lease | Scalar relays cannot hide imported leases. Dynamic and uncertain store-owned cases retain suspension, including empty dynamic topology. Roots, native lease handoff, interruption and cleanup remain unconditional. |
| Scope reuse | Repeated sidecar resolution removed; the scope address belongs to a once-published, never-replaced sidecar | Cross-instance dispatch uses the callee's captured scope. Close does not recycle tokens or sidecars. All generation and optional-state atomics remain. Other entry paths retain lazy initialization. |
| Reservation order | Fallback-map probe removed on an exact inline match under the existing mutex | A different invocation ID cannot use the inline entry. Nested swaps, fallback lookup and cleanup remain. No new map or cache is introduced. |
| Activation-local context | Repeated root lookup removed only within one native loop with the same control frame | Re-entry gets a separate activation. Missing root identity and changed frames use the general lookup. Callee identity is never promoted to root identity. GC ownership is not cached. |

No step admits additional native scheduler paths or permits host code on a
foreign stack. An indefinitely blocking callback still runs as ordinary Go.
Cancellation, trap publication and lease reacquisition retain the old order.
The existing recursion/entry controls remain; the context snapshot adds fixed
stack storage per already-required native entry, not state per repeated call.
Reservation fallback cardinality and lifetime remain tied to live nested
invocations, with removal on unwind. No cache grows with callback count.
High callback frequency no longer creates Wago token boxes on the concrete
scalar path; host code can still choose to allocate or retain its own values.
Neither retained slices nor arbitrary Go host behavior are used as a proof of
native safety. Direct frame views and scheduler segments remain disabled.

## Final results

[The measurement table](host-caller-benchmarks.csv) contains sample counts,
medians, ranges, bytes and allocations for every benchmark checkpoint, including
all matched loop counts. Its commit column identifies the runtime code; the
final test-only additions do not change dispatch. Benchmarks ran without large
concurrent local builds. The CPU profile's separately timed sample is not mixed
into the five-sample latency medians.

Memory-0 repeated-call estimates subtract the matched guest loop at 1,024
iterations and divide by 1,024. Bytes and allocations in this table are per
callback, not per 1,024-call public invocation:

| Stage | ns/call | B/call | allocs/call |
|---|---:|---:|---:|
| Fresh PR #588 baseline | 227.1 | 64 | 1 |
| Concrete caller | 159.6 | 0 | 0 |
| No-GC suspension fast path | 139.3 | 0 | 0 |
| Scope/state cleanup | 124.9 | 0 | 0 |
| Reservation lookup | 132.1 | 0 | 0 |
| Context caching | 115.6 | 0 | 0 |
| Direct frame experiment | Not enabled | — | — |
| Final combined, fresh sweep | 115.8 | 0 | 0 |

These are sequential checkpoints, not an additive attribution model. In
particular, the same-checkpoint legacy/concrete comparison at the API stage is
184.0 versus 159.6 ns/call; the fresh baseline was slower. Reservation ordering
has a measured microbenchmark win but no isolated full-loop win.

### Public invocation and matched loop

All values below are median ns per public invocation, memory-0. Both controls
use the same compiled, host-capable `run(count, host)` export and `Invoke` API.
Each result is checked against the requested count.

| Count | Baseline host | Baseline guest | Final legacy host | Final concrete host | Final concrete guest |
|---|---:|---:|---:|---:|---:|
| 0 | 124.2 | 115.5 | 136.7 | 136.4 | 137.8 |
| 1 | 442.7 | 126.9 | 391.1 | 356.0 | 138.7 |
| 8 | 1862 | 134.2 | 1413 | 1199 | 143.2 |
| 64 | 13395 | 164.6 | 9148 | 7718 | 166.6 |
| 1024 | 233111 | 604.2 | 142821 | 119186 | 592.4 |

The final concrete fixed invocation median is 136.4 ns (135.5–137.1), versus
124.2 ns (117.4–126.1) at baseline. That fixed-cost increase is not hidden by
using a cheaper zero-host export. A linear fit of all five host-minus-guest
medians gives 115.7 ns per additional call, close to the endpoint estimate.

The retained simple one-call API benchmark measures 353.3 ns concrete
(352.1–359.4), 0 B and 0 allocations; legacy is 395.8 ns (393.9–402.9), 64 B
and one allocation. Baseline was 416.4 ns (413.1–430.0). This one-call fixture
is reported separately and is not subtracted from a different execution path.

The final concrete 1,024-call range is 119098–120587 ns; legacy is
141725–143608 ns. Final legacy repeated cost is 138.9 ns/call with 64 B and one
allocation per callback. All final concrete scalar arities and GC-domain loop
cases report zero Wago allocations after setup.

### Memory count and parallel throughput

| Memories | Concrete host median ns/1024 calls (range) | Guest median ns | Approx. ns/call |
|---|---:|---:|---:|
| 0 | 119186 (119098–120587) | 592.4 | 115.8 |
| 1 | 121407 (121020–125707) | 620.1 | 118.0 |
| 4 | 121649 (119928–122373) | 632.6 | 118.2 |

The prior native-context reuse path remains in place. Full memory-directory
refresh is linear in memory count, but an unchanged private host return does
not refresh it. These samples show no large per-callback memory-count growth.
The four-memory fixture explicitly enables multi-memory and is skipped only
on products that do not support that additional feature.

Sixteen independent memory-0 instances measure 15946 ns per 1,024-call public
invocation (15711–16333) for concrete callbacks, with a 99.58 ns guest control.
That is 64.6 million callbacks/s in aggregate, not 15.5 ns single-thread
latency. Legacy is 34722 ns (34686–35034), with a 103.2 ns guest control, or
29.6 million callbacks/s. Baseline host time was 37027 ns (36767–38002).
The complete per-memory parallel ranges are in the CSV.

### GC-capable fallback and arities

The actual GC fallback remains active in every row below. These are ns per
1,024-call public invocation, including fixed invocation and guest work, not
isolated suspension timings:

| Concrete path | Median ns (range) | B/op | allocs/op |
|---|---:|---:|---:|
| Local WasmGC | 215934 (215316–219360) | 0 | 0 |
| Imported GC domain, no local collector | 171994 (170608–172313) | 0 | 0 |
| Dynamic GC domain | 186272 (184788–189041) | 0 | 0 |

The final combined imported-domain result is below the concrete-API reference
measurement; the intermediate no-GC layout tradeoff is not a final fallback
regression. Each local-GC invocation constructs a guest object before the loop
to require a real collector. Imported and dynamic cases assert actual admission
flags and domain ownership, not just a scalar signature.

The direct-frame experiment stays disabled. Existing buffers give these public
one-call medians (ranges), ns/op:

| Arguments -> results | Legacy | Concrete |
|---|---:|---:|
| 0 -> 0 | 385.2 (383.5–389.8) | 341.0 (339.3–343.9) |
| 1 -> 0 | 381.4 (379.5–384.8) | 345.4 (343.1–354.5) |
| 1 -> 1 | 389.5 (385.1–392.7) | 354.3 (353.8–354.8) |
| 4 -> 1 | 397.8 (396.2–400.2) | 365.5 (360.9–368.2) |
| 8 -> 4 | 422.6 (419.3–425.0) | 385.1 (383.3–391.3) |
| 16 -> 8 | 458.6 (455.6–460.9) | 417.7 (414.6–422.0) |
| 64 -> 64 | 757.0 (755.9–760.6) | 713.5 (711.5–720.1) |

Legacy allocates 64 B/one object in each arity; concrete allocates zero.
These comparisons measure caller boxing, not an unimplemented frame-view path.

## Final profile and supported follow-up work

The final concrete, memory-0, 1,024-call no-op-host CPU profile has 13.29 seconds
of samples. Ranked by **flat**, non-overlapping CPU attribution:

| Rank | Symbol | Flat CPU | Cumulative CPU |
|---|---|---:|---:|
| 1 | Unresolved `runtime._ExternalCode` | 14.00% | 14.00% |
| 2 | `dispatchSyncHostScalar` | 8.88% | 16.63% |
| 3 | `hostLoopActivation.dispatch` | 8.43% | 51.47% |
| 4 | `runtime.exitsyscall` | 6.40% | 7.60% |
| 5 | `enterNativeRaw` | 5.19% | 5.19% |
| 6 | `Engine.callWithHostLoop` | 4.36% | 79.46% |

`loadTrap` is 3.99% flat; `resumeNative` is 3.46% flat / 16.48% cumulative.
Scope construction is 2.71% flat / 4.59% cumulative. The remaining sidecar
lookup is 2.63% flat. Cumulative percentages overlap and must not be added.
Unresolved external samples are not assigned to a particular JIT instruction
or trampoline without further symbolization.

Generic GC suspension and repeated context/reservation lookup are absent from
the private hot profile. Sampled allocation space is 4.68 MiB from runtime,
compiler, benchmark and profiler setup; there are no callback-token boxes.
The sampled cancellation context comes from `testing.B.runN`, not dispatch.
The mutex profile records 649 us of Go-runtime delay, not Wago global activation
bookkeeping.

Known likely improvement to measure next: carry the already-published sidecar
through the remaining activation/lease helpers. The 2.63% flat lookup cost is
visible, and the stable publication proof is already established. Keep all
ownership locks and generation atomics.

Experimental idea supported by this profile: reduce intermediate token-value
moves in scalar scope construction. Line attribution places 620 ms flat at the
constructor call site. Go disassembly shows an eight-word return spill followed
by a 64-byte stack copy before the concrete call. A better constructor shape
must preserve the immutable by-value token and exception cleanup; no speedup is
claimed without a new measurement. Do not replace it with mutable token pooling.

Scheduler transitions are a visible remaining cost, but still have no complete
continuation/progress proof for arbitrary host-capable execution. The segment
analyzer remains test infrastructure only. No scheduler speedup is claimed.

## Files and validation

Production changes are split into measured commits:

| Commit | Topic | Main implementation files |
|---|---|---|
| `1b67194a4` | Portable scheduler fixture | `src/wago/host_scheduler_bench_test.go` |
| `05a8f0b89` | Concrete API, direct allocation-free dispatch and helper compatibility | `src/wago/hostcall.go`, `api.go`, `instantiate.go`, `registry.go`, `plugin_plan.go`, `plugin_call_gate.go`, `managed_instances.go`, `globals.go`, `instruction_runtime.go`; generated `wago.go` |
| `72832b16a` | Proven no-domain suspension branch | `src/wago/reference_store.go`, `host_execution.go` |
| `8ab8d0df7` | Strict Go/TinyGo binding footprint | `src/wago/footprint_test.go` |
| `40bda73c5` | Once-published scope pointer | `src/wago/hostcall.go` |
| `b70909137` | Inline reservation first | `src/wago/instance_native_context.go` |
| `ae19a6ec0` | Activation-local root context | `src/wago/host_execution.go`, `hostcall.go` |

All enabled runtime changes are production paths with the conservative
fallbacks described above. New benchmark variants are in
`host_roundtrip_bench_test.go`, `caller_arity_bench_test.go`, `caller_test.go` and
`host_activation_test.go`. `ARCHITECTURE.md` and `CONTRIBUTING.md` describe the
API, measurement method and race-run exit-delay setting.

Added or expanded tests cover:

- `TestCallerRetainedAndNested`, `TestCallerCapabilityHelpers`: immutable
  generations; stale caller A during callback B; nested B expiry and A
  restoration; zero caller; resolver, invoker, invocation context, watcher,
  guest storage, GC result operations and live externref read/release denial.
- `TestCallerScalarDispatchAllocations`, `TestCallerImportedStart`,
  `TestCallerReexport`, `TestCallerConcurrentIndependentInstances`: public entry
  variants, zero allocations and 16 independent standard-Go parallel workers.
- `TestHostInvocationContextCrossInstanceChain`,
  `TestHostLoopActivationContextNesting`,
  `TestHostLoopActivationDoesNotCacheCalleeAsRoot`: A -> host -> B -> host -> A,
  exact callee scope, parent/reservation restoration and distinct cache lifetime.
- `TestHostGCSuspensionAdmission`,
  `TestScalarCrossInstanceRelaySuspendsAllProducerGCDomainsForHostCollection`:
  private/local/imported/dynamic/uncertain admission and actual root-owned leases
  in both callback representations.
- `TestHostContextResumeMatchesForcedRestore`: both APIs versus forced general
  suspension/restoration, including reads, writes, nested memory growth, globals,
  parked export, Go GC, panic, HostExit, HostTrap and cancellation.
- `TestPluginGCHostImportsNonNullRoundTripAndZeroCopyWrite` and
  `TestCallerResolverInvocationContextContract`: both APIs with exact GC results,
  collection, guest storage and callback-context expiry. Existing dynamic-domain,
  scheduler admission, generation exhaustion and GC frame-root tests remain.

The complete `src/wago` suite and its race suite ran at each production
checkpoint, with timings recorded above. Final commands and results:

| Command | Result |
|---|---|
| `go test ./...` | Runtime and integration packages pass; three local external-tool failures below |
| `(cd bench && go test ./...)` | Pass, including semantic corpus and suite |
| `GORACE=atexit_sleep_ms=0 go test -race -count=1 ./src/wago ./src/core/runtime ./tests/integration/runtimeconcurrency` | Pass: 20.545 s, 1.188 s, 0.169 s |
| `go test -count=1 -tags wago_guardpage ./src/core/runtime ./src/wago` | Pass: 0.432 s, 2.983 s |
| `make test-fuzz FUZZTIME=5s` | All four bounded gates pass |
| `go test ./src/wago -run '^$' -fuzz '^FuzzCompiledCodecGeneratedValidModules$' -fuzztime=5s` | Pass: 122,993 executions |
| `go test ./src/wago -run '^$' -gcflags='-m=2'` | Escape and inlining output inspected at API, suspension and cache stages |
| `go build -gcflags='-m=2' ./src/wago` | Pass; concrete dispatch method value does not escape |
| `go generate ./...` | Pass; generated facade unchanged in final check |

Full tests use the pinned spec interpreter at
`.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm`, with
`WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa` and
`WAGO_SPEC_INTERPRETER` set to that executable's absolute path.
`GORACE=atexit_sleep_ms=0` removes only process-exit delay, not race checks.
The final ordinary `src/wago` package run passed in 6.931 s.

The local all-repository failures are
`TestWineCmdBootstrapDownloadsVerifiesAndExecutesInstaller` (installer download
unavailable), `TestBuildTinyGoEmbedsArtifactWithoutCompiler`, and
`TestBuildTinyGoStripsByDefault` (both link with duplicate `tinygo_task_exit`).
They reproduce the checkout's local external-tool failures; they are not
hidden or counted as passes. The 59-second standalone test package is dominated
by those external TinyGo builds, not a new runtime or compiler hot-path delay.

The final runtime commit passed the
[full CI matrix](https://github.com/wago-org/wago/actions/runs/34478366701),
including Darwin/amd64, both Windows targets, all TinyGo lanes, Linux/arm64,
race, concurrency, fuzz and Core v2/v3 conformance. The initial fixture-only
commit and the scope checkpoint also passed full native matrices.

### Reproduce the measurements

```bash
go test ./src/wago -run '^$' \
  -bench '^Benchmark(InvokeHostFuncDirect|InvokeCallerHostFuncDirect|HostRoundtripLoop|HostRoundtripLoopCaller)$' \
  -benchmem -benchtime=500ms -count=5
go test ./src/wago -run '^$' \
  -bench '^Benchmark(CallerArity|CallerGCLoop|CallerDomainLoop)$' \
  -benchmem -benchtime=500ms -count=5
go test ./src/wago -run '^$' \
  -bench '^BenchmarkCurrentInvocationReservation$' \
  -benchmem -benchtime=2s -count=10
go test ./src/wago -run '^$' \
  -bench '^BenchmarkHostRoundtripLoopCaller$/mem0/parallelfalse/host1/n1024$' \
  -benchmem -benchtime=10s -cpuprofile=/tmp/wago-caller-cpu.pprof \
  -memprofile=/tmp/wago-caller-alloc.pprof -mutexprofile=/tmp/wago-caller-mutex.pprof
```

The imported/local/dynamic fallback comparison also used ten 2-second samples
from separate reference and candidate binaries. CPU, allocation and mutex
profiles were collected after every meaningful production step. The public
single-call benchmark is selected separately from loop sub-benchmark filters
so it is never silently excluded by a slash-qualified regular expression.
