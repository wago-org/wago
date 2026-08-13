# Generated native-code size results

This is the measurement ledger for
[the generated native-code size plan](generated-native-code-size-plan.md). It
records raw native bytes separately from compile latency, allocations, and
execution. Results from different architectures or commits are not combined.

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
```
