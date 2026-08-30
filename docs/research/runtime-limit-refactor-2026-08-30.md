# Runtime limit refactor measurements — 2026-08-30

Host: Linux/amd64, AMD Ryzen 7 8845HS. The first pass used `GOMAXPROCS`
default and `-benchmem -benchtime=200ms -count=3` unless stated otherwise. Its
baseline is `origin/main`; its changed column is the initial basedata-quota
layout before the follow-up relocation experiment.

| Benchmark | Baseline median | Initial changed median | Delta | Allocation delta |
|---|---:|---:|---:|---:|
| `BenchmarkRuntimeInstantiateSmallScalar` | 2,277 ns/op | 2,196 ns/op | -3.6% | 1,504 to 1,520 B/op; 10 allocs unchanged |
| `BenchmarkCompileSmallScalar` | 19,118 ns/op | 19,080 ns/op | -0.2% | 14,069 to 14,085 B/op; 63 allocs unchanged |
| `BenchmarkInvokeHostFuncDirect` | 713.4 ns/op | 744.4 ns/op | +4.3% | 736 B/op and 3 allocs unchanged |
| `BenchmarkStagedGCStructBasicPublicToken` | current only: 869.3 ns/op | — | — | 1 B/op, 0 allocs/op |

An initial seven-sample, 500 ms host-call rerun measured a 700.0 ns/op baseline
median and 734.7 ns/op changed median (+5.0%), with unchanged allocations. The
small host-call generated fast path remained the 64-slot inline layout; wide
spill code is selected only for signatures above 64 slots. The initial quota
implementation instead grew the instance context from 112 to 120 bytes and
basedata from 288 to 304 bytes, adding one context-bind load/store on every
public entry.

A follow-up experiment moved the quota into byte 20 of the existing 24-byte
per-instance memory-directory entry. A single-memory instance allocates that
cold entry only when the quota is nonzero; `memory.grow` reads it through the
already-rebound directory pointer. This restores 112-byte instance contexts and
288-byte basedata, and it adds no operation to an ordinary host-call entry.

Three separately built benchmark binaries compared `origin/main`, the initial
basedata-quota layout, and the directory-quota experiment. Nine rotated rounds
used CPU 2, `GOMAXPROCS=1`, and 2-second samples:

| Host-call layout | Median | Mean | Range | Allocations |
|---|---:|---:|---:|---:|
| `origin/main` | 712.2 ns/op | 716.8 ns/op | 697.5–739.0 ns/op | 736 B/op, 3 allocs |
| Initial basedata quota | 719.3 ns/op | 726.7 ns/op | 698.6–757.2 ns/op | 736 B/op, 3 allocs |
| Directory quota | 713.5 ns/op | 714.5 ns/op | 693.0–731.7 ns/op | 736 B/op, 3 allocs |

By medians, the initial layout was +1.0% against `origin/main`; the directory
layout was +0.18%. The directory layout improved 0.81% against the initial
layout by medians and 1.68% by means. A separate twelve-pair, 700 ms interleaved
run measured 1,125.0 ns/op before and 1,122.5 ns/op after (-0.22% median), with a
-0.87% paired geometric-mean change. Absolute timings changed with laptop
frequency state, but both interleaved comparisons reject the original +5%
result as a stable regression. The directory layout is retained because it also
restores the smaller ABI and confines quota work to `memory.grow`.

Correctness overflow paths were tested at 65 host slots, 160 simultaneous public
GC handles/argument roots, 1,025 live native GC roots, 100 `array.new_fixed`
elements, 300 shared-memory importers, and instance metadata above 1 MiB.

## Scalar host-dispatch allocation experiment

The low-level native-to-Go-to-native host loop measures about 164.7 ns/op with
zero allocations, while the public scalar `HostFunc` path initially retained
736 B/op and three allocations. Allocation profiles attributed those objects to
the callback GC-result scratch, boxing the callback-scoped `HostModule`, and the
closure returned while suspending the GC invocation lease.

The retained optimization:

- classifies each immutable import binding as scalar or reference-bearing at
  instantiation;
- stores one compact 24-byte binding containing the host function, exact type
  descriptor pointer, import index, and class;
- skips GC argument/result token scratch for scalar signatures; and
- represents GC-lease suspension as an explicit stack value instead of an
  escaping resume closure.

Twelve rotated one-second samples on CPU 2 with `GOMAXPROCS=1` compared the
pre-change and branch-specialized binaries:

| Scalar public host call | Median | Mean | Allocation |
|---|---:|---:|---:|
| Before | 779.2 ns/op | 776.3 ns/op | 736 B/op, 3 allocs |
| Scalar/reference branch | 590.2 ns/op | 590.0 ns/op | 112 B/op, 1 alloc |
| Stored dynamic callback | 592.5 ns/op | 594.9 ns/op | 112 B/op, 1 alloc |

The scalar/reference branch improved the median by 24.3%. Storing a function
callback per binding was 0.39% slower by medians and 0.84% slower by means than
the predictable branch, so the branch is retained. A final compact-binding
rerun measured 785.9 ns/op before and 590.9 ns/op after (-24.8%), with a -24.7%
paired geometric-mean change. The remaining 112-byte allocation is the immutable
callback-scoped `HostModule` value; removing it from the existing API would need
a non-reusable stale-safe token representation or a separate module-free fast
host API.
