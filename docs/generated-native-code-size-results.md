# Generated native-code size results

This is the measurement ledger for
[the generated native-code size plan](generated-native-code-size-plan.md). It
records raw native bytes separately from compile latency, allocations, and
execution. Results from different architectures or commits are not combined.

## AMD64 baseline: 2026-08-13

Environment:

```text
host: hub
cpu: AMD Ryzen 7 7800X3D (8 physical cores)
os/arch: Linux/amd64
Go: go1.22.2
code: 8db8ee2b plus AMD64 ledger commit 230c3b2d
bounds: explicit
function workers: p1
benchtime: 500ms compile; 1s execution
samples: 5
```

### Baseline medians

| Workload | Native code | CompileFull p1 | Compile B/op | Allocs/op | Execution |
| --- | ---: | ---: | ---: | ---: | ---: |
| `many_funcs` | 9,704 B | 362,341 ns/op | 218,265 | 683 | 7.954 ns/op |
| `json-as` | 66,131 B | 1,786,363 ns/op | 350,581 | 2,147 | serialize 23,807 ns/op; deserialize 42,679 ns/op |

Execution remained at zero B/op and zero allocs/op. The baseline commands ran
one timed workload at a time on the native AMD64 host.

### Native-ledger opportunity snapshot

| Byte class | `many_funcs` | `json-as` |
| --- | ---: | ---: |
| Total native bytes | 9,704 | 66,131 |
| Function-owned bytes | 7,817 | 65,835 |
| Inter-function alignment | 1,887 | 296 |
| Module-owned islands/padding | 0 | 0 |
| Host adapters | 17 | 778 |
| Adapter-to-internal padding | 15 | 54 |
| Internal function bytes | 7,785 | 65,003 |
| Physical frame-adjustment reservations | 4,214 | 602 |
| Dead frame-reservation bytes | 4,206 | 234 |
| Retained branch-fold holes | 0 | 320 |
| Total proved-dead reservation bytes | 4,206 | 554 |

The already-proved dead subset is 43.34% of `many_funcs` and 0.84% of
`json-as`. Unconditional function alignment adds another 19.45% to
`many_funcs`. These categories are subsets or ownership classes, not additive
predictions: compaction changes every following function offset and its padding.

Stats-off output and allocation counts remained exact: 9,704/66,131 code bytes,
218,265/350,581 B/op, and 683/2,147 allocs/op. A second five-sample run put
`json-as` compile time within noise of baseline; `many_funcs` was 2.7% slower in
that short run, with no generated-byte or allocation change, so no latency claim
is made for the opt-in ledger.

## ARM64 baseline: 2026-08-13

Environment:

```text
host: Apple M4 Max
os/arch: darwin/arm64
Go: go1.26.5
baseline code: 7f7a5f46f03578a7bc59c224e3ffe32f5e3eba47
plan-only commit: f0004a05
bounds: explicit
function workers: p1
benchtime: 500ms
samples: 5
```

The baseline was taken before native-ledger implementation. The plan-only
commit changes no compiler code.

### Baseline medians

| Workload | Native code | CompileFull p1 | Compile B/op | Allocs/op | Execution |
| --- | ---: | ---: | ---: | ---: | ---: |
| `many_funcs` | 28,984 B | 272,955 ns/op | 195,167 | 380 | 15.78 ns/op |
| `json-as` | 77,516 B | 1,016,880 ns/op | 351,199 | 1,273 | serialize 18,299 ns/op; deserialize 37,813 ns/op |

Execution remained at zero B/op and zero allocs/op.

### Native-ledger opportunity snapshot

These values use the public `wago.Compile` path with `WAGO_EXPLAIN=size`, rather
than the lower-level explain tool's narrower backend configuration.

| Byte class | `many_funcs` | `json-as` |
| --- | ---: | ---: |
| Total native bytes | 28,984 | 77,516 |
| Function-owned bytes | 28,980 | 77,268 |
| Inter-function alignment | 4 | 248 |
| Host adapters | 28 | 844 |
| Adapter-to-internal padding | 4 | 52 |
| Internal function bytes | 28,948 | 76,372 |
| Physical frame-adjustment reservations | 7,224 | 1,032 |
| Dead frame-reservation bytes | 7,216 | 688 |
| Retained branch-fold holes | 0 | 52 |
| Retained store/load NOPs | 12 | 28 |
| Total proved-dead reservation bytes | 7,228 | 768 |

The already-proved dead subset is 24.94% of `many_funcs` and 0.99% of
`json-as`. These are opportunity measurements, not predicted final reductions:
compaction changes later alignment and branch ranges, and a finalizer must still
prove complete metadata remapping.

### Stats-off neutrality check

After adding the ARM64 native ledger, with collection disabled as in normal
compilation:

| Workload | Native code | CompileFull p1 median | Compile B/op | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| `many_funcs` | 28,984 B | 268,673 ns/op | 195,167 | 380 |
| `json-as` | 77,516 B | 989,634 ns/op | 351,199 | 1,273 |

The emitted byte counts and allocation counts are identical. The short latency
runs were 1.57% and 2.68% lower respectively; this is treated as noise/evidence
against regression, not as a speedup claim. Execution was not rerun for this
stats-only change because codegen-neutrality tests compare every emitted byte.

### Immutable policy foundation

The first checkpoint added an immutable, at-most-64-bit optimization selection
and the `Speed`, `Balanced`, `Size`, and `Embedded` policy vocabulary. The next
checkpoint threaded that selection through every catalogued ARM64 lowering
decision. Independent module compilations no longer install temporary values in
package globals or hold the optimization-binding lock for the duration of
codegen. Hot lowering sites use pre-resolved option tokens: an initial
string/map implementation failed the serial compile-time gate and was replaced
before commit.

The paired comparison below uses the policy-foundation commit `2ee02836` as the
before point and the policy-threaded working tree as the after point. Both were
measured on the same host with Go 1.26.5, 500 ms samples, count 5. The concurrent
benchmark fixes `-cpu=8`; each module itself uses one function worker.

| Workload | Metric | Before median | After median | Change |
| --- | --- | ---: | ---: | ---: |
| `many_funcs` | CompileFull p1 | 275,854 ns/op | 270,053 ns/op | -2.10% |
| `json-as` | CompileFull p1 | 999,261 ns/op | 990,215 ns/op | -0.91% |
| `many_funcs` | 8-way module throughput | 230,746 ns/op | 43,036 ns/op | -81.35% (5.36x throughput) |
| `json-as` | 8-way module throughput | 653,767 ns/op | 148,473 ns/op | -77.29% (4.40x throughput) |

Native output stayed byte-identical at 28,984 B and 77,516 B. The public compile
path also dropped one allocation per operation: 380 to 379 for `many_funcs` and
1,273 to 1,272 for `json-as`, with a small corresponding B/op reduction. A race
test interleaves 128 compilations with opposing frame policies and verifies that
each produces its serial reference bytes without changing process defaults.

### Identity finalization seam

The ARM64 function-completion path now calls a shared zero-copy identity
finalizer and routes these function-relative offsets through its old-to-new map:

```text
internal entry
module direct-call relocation sites
adapter continuation
GC adapter continuation
GC call-return sites
```

Frame adjustment sites remain explicit compiler fields, while NOP holes proved
by branch folding and same-register store/load forwarding are retained in reused
branch-target scratch. `WAGO_FINALIZE=0` bypasses the seam, and
`WAGO_FINALIZE_VALIDATE=1` enables exhaustive site/label validation for tests
and debugging. The default path does not allocate or copy a function image.

Paired on/off measurements used the same working tree and the regular p1
commands below:

| Workload | Finalizer off median | Identity finalizer median | Change | B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| `many_funcs` | 280,221 ns/op | 279,200 ns/op | -0.36% | 195,144 / 379, unchanged |
| `json-as` | 998,834 ns/op | 1,005,071 ns/op | +0.62% | 351,174 / 1,272, unchanged |

Native output remained byte-identical at 28,984 B and 77,516 B. These deltas are
treated as noise. Shrinking is still disabled: internal PC-relative branches,
ADR references, and jump-table edges must become explicitly repatchable before
the first deletion is allowed.

### Opt-in ARM64 compaction checkpoint

The ARM64 finalizer can now compact already-proved dead ranges in place with
`WAGO_COMPACT=1`. It removes the unused words in the three-instruction frame
reservation, removes retained branch-fold holes, remaps function metadata, and
re-encodes every recognized PC-relative branch, ADR reference, and jump-table
entry. Explicit jump-table and plugin fragments prevent instruction scanning
through embedded or opaque bytes. The fixed deletion budget is eight ranges per
function; a function that exceeds it or contains opaque plugin output retains
the old size-preserving path.

Compaction is deliberately not the default yet. Frame deletion changes final
body and loop offsets after the current emission-time alignment decisions. A
paired execution check found a repeatable scalar `json-as` serialize regression
above 1% while the SIMD variant improved substantially. Final-layout-aware,
objective-owned alignment is therefore the next gate before default enablement.

#### Native bytes and compile cost

These medians compare `WAGO_COMPACT=1` with `WAGO_COMPACT=0` on the same working
tree, using five 500 ms p1 samples:

| Workload | Compaction off | Compaction on | Native change | Compile p1 off | Compile p1 on | Compile change | B/op / allocs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `many_funcs` | 28,984 B | 24,168 B | -16.62% | 549,387 ns/op | 534,399 ns/op | -2.73% | 195,145 / 379, unchanged |
| `json-as` | 77,516 B | 76,780 B | -0.95% | 2,053,737 ns/op | 1,948,710 ns/op | -5.11% | 351,179 / 1,272, unchanged |

The compile-time improvement is not claimed as a durable speedup: on macOS the
public p1 benchmark is dominated by executable-buffer mapping syscalls and
varies materially between runs. A CPU profile attributed about 7 microseconds
per `many_funcs` compile to finalization. The allocation gate is conclusive:
compaction adds neither bytes nor allocations per compile. A temporary
slice-backed offset map failed this gate and was replaced by fixed-capacity
storage before this checkpoint.

The 4,816-byte `many_funcs` reduction is smaller than its 7,216-byte frame-hole
opportunity because each compacted tiny function is still aligned to 16 bytes;
inter-function padding grows by about 2,400 bytes. This directly motivates the
alignment phase.

#### Paired execution check

The following medians use strictly alternating one-second compact/off runs.
Every benchmark remained at zero B/op and zero allocs/op.

| Workload | Compaction off | Compaction on | Change |
| --- | ---: | ---: | ---: |
| `many_funcs.run` | 17.24 ns/op | 17.38 ns/op | +0.84% |
| `json-as.serializeN` | 18,635 ns/op | 18,856 ns/op | +1.19% |
| `json-as.deserializeN` | 38,751 ns/op | 39,097 ns/op | +0.89% |
| `json-as-simd.serializeN` | 23,640 ns/op | 22,388 ns/op | -5.30% |
| `json-as-simd.deserializeN` | 48,884 ns/op | 49,357 ns/op | +0.97% |

Adversarial unit coverage includes B/BL, conditional, compare-and-branch,
test-and-branch, ADR, deletion-boundary fragment transitions, and jump-table
delta remapping. The ARM64 package passes normal, inventory-validation, and race
test modes. Targeted runtime corpus and fuzz-regression execution also pass with
both compaction and exhaustive validation enabled.

### Objective-aware ARM64 alignment checkpoint

Alignment is now owned by the immutable per-compilation policy rather than
hard-coded emission calls. The low-level ARM64 compile API accepts an explicit
objective; nil remains Balanced. Speed retains 16-byte function, internal-entry,
and loop alignment. Size and Embedded retain only ARM64's mandatory four-byte
instruction alignment. Balanced applies an exact padding budget:

```text
addressable entry: retain 16-byte alignment
looping, recursive, call-making, or >=256-byte body:
  optional padding <= min(12, wasmBodyBytes / 16)
other tiny leaf:
  four-byte alignment
```

This is deliberately static and allocation-free, so the serial compiler can
continue emitting directly into the module-owned executable buffer. Serial and
parallel compilation use the same retained layout flags and are byte-identical
for every objective.

With compaction disabled, Balanced reduces `json-as` from 77,516 to 77,436
bytes (-0.10%) and leaves `many_funcs` at 28,984 bytes: its maximal 96-byte leaf
functions happened to be naturally 16-byte sized. With opt-in compaction, those
same leaves become densely packed and `many_funcs` reaches 21,756 bytes
(-24.94% from baseline). `json-as` reaches 77,036 bytes (-0.62%). The extra 12
bytes beyond the prior compaction checkpoint come from deleting proved-dead
same-register store/load NOPs.

Functions containing loops currently retain the size-stable path. This keeps
emission-time loop alignment valid until loop padding becomes an explicit
relaxable fragment. Explicit ADR site markers and a span-copy fast path avoid a
second machine-instruction walk for ordinary loop-free functions.

#### Alignment execution check

A detached `a2e4b092` worktree provided the pre-alignment comparison. Five
strictly alternating one-second samples used `WAGO_COMPACT=0` on both trees:

| Workload | Before median | Adaptive Balanced median | Change |
| --- | ---: | ---: | ---: |
| `many_funcs.run` | 17.51 ns/op | 17.38 ns/op | -0.74% |
| `json-as.serializeN` | 18,664 ns/op | 18,774 ns/op | +0.59% |
| `json-as.deserializeN` | 38,661 ns/op | 37,385 ns/op | -3.30% |

All remained at zero B/op and zero allocs/op. The three-workload geomean
improved; no individual regression exceeded the 1.5% investigation gate.

#### Why compaction remains opt-in

An in-package benchmark now measures decoded-module compilation with compaction
on and off in the same process, avoiding frontend work and reducing macOS
MAP_JIT frequency noise. Five 500 ms samples produced:

| Workload | Off median | On median | Change | B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| `many_funcs` | 188,280 ns/op | 193,158 ns/op | +2.59% | 133,665 / 342, unchanged |
| `json-as` | 668,298 ns/op | 750,050 ns/op | +12.23% | 291,674 / 1,003, unchanged |

The loop-free workload is inside the proposed 3% Balanced gate; `json-as` is
not. Compaction therefore remains behind `WAGO_COMPACT=1`. The next finalizer
work must make loop alignment symbolic and eliminate the remaining remap cost
before changing the default.

### Commands

```sh
cd bench

go test -run '^$' \
  -bench '^BenchmarkCompileFullWorkers$/^many_funcs$/^p1$' \
  -benchmem -benchtime=500ms -count=5 .

go test -run '^$' \
  -bench '^BenchmarkCompileFullWorkers$/^json-as$/^p1$' \
  -benchmem -benchtime=500ms -count=5 .

go test -run '^$' -bench '^BenchmarkExec$/^many_funcs' \
  -benchmem -benchtime=500ms -count=5 .

go test -run '^$' -bench '^BenchmarkExec$/^json-as' \
  -benchmem -benchtime=500ms -count=5 .

WAGO_EXPLAIN=size go test -run '^$' \
  -bench '^BenchmarkCompileFullWorkers$/^many_funcs$/^p1$' \
  -benchtime=1x -count=1 .

WAGO_EXPLAIN=size go test -run '^$' \
  -bench '^BenchmarkCompileFullWorkers$/^json-as$/^p1$' \
  -benchtime=1x -count=1 .

go test -run '^$' \
  -bench '^BenchmarkCompileMultiModuleThroughput$/^(many_funcs|json-as)$/^p1$' \
  -benchmem -benchtime=500ms -count=5 -cpu=8 .

WAGO_COMPACT=1 WAGO_FINALIZE_VALIDATE=1 go test -count=1 \
  ./src/wago -run '^(TestCorpusExecution|TestFuzzRegressionCorpus)$'

go test -run '^$' -bench '^BenchmarkCompileModuleCompactionArm64$' \
  -benchmem -benchtime=500ms -count=5 \
  ./src/core/compiler/backend/railshot/arm64
```
