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
