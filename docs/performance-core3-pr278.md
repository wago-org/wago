# PR #278 Core 3 optimization campaign

Date: 2026-07-19

## Scope and environment

This campaign measured the current WebAssembly Core 3 branch from initial SHA
`ad44de19e5f33186458fdb02fa28d9bcfbb6f830` on Linux/amd64, AMD Ryzen 7
8845HS, 16 logical CPUs, performance governor. Performance A/B runs used Go
1.24.4; CI-equivalent correctness checks used Go 1.22.12. Single-threaded A/B
runs were pinned to CPU 0 with `GOMAXPROCS=1`. Where laptop thermal/frequency
ordering produced contradictory block-ordered results, separate base and
candidate test binaries were alternated for 20 rounds.

The pinned MoonBit JSON artifact was unavailable: installed MoonBit was
`0.1.20260713`, while the required version was `0.1.20260703`. No Starshine
artifact was configured. Those lanes were therefore reported as unavailable,
not replaced by unrelated modules.

## Baseline repairs and attribution

Before optimization, the branch needed two correctness/CI repairs:

- `267e39d0`: remove stale Core 3 helpers so pinned staticcheck/lint passes.
- `34b264ce`: initialize logically grown guarded imported memory through
  `HostBytes`, fixing the reproduced guard-page panic.

Test-only stage attribution was added in `7a989ff8` and extended with explicit
segment-state and native-code-size metrics in `ebfc3f65` and `67437d71`.
Representative fixtures are tiny, json-as, wasm3, SQLite, Ruby, and esbuild,
with optional MoonBit JSON and Starshine lanes.

Baseline attribution found:

- function-body validation dominated large-module validation (about 456 ms for
  Ruby and 299 ms for esbuild in the initial cold samples);
- declaration validation for esbuild was about 15.5 ms;
- required-feature, module-fact, frontend-admission, and segment-state analyses
  each repeated code-section walks;
- eager frontend diagnostic construction dominated successful admission
  allocation;
- Railshot lowering remained the largest cold-compile CPU/allocation stage;
- generated execution, host calls, memory access, and WasmGC helper steady-state
  paths were already allocation-free in the measured hot loops.

## Accepted changes

| SHA | Change | Repeatable evidence |
|---|---|---|
| `3a547731` | Defer const-expression instruction diagnostics | esbuild admission -7.54% time, -56.20% B/op, -48.81% allocs; esbuild full-compile alloc count -32.33% |
| `5baf2670` | Defer active data-segment diagnostics | esbuild admission -9.18% time and -95.17% allocs; full-compile alloc count -47.70% relative to the preceding revision |
| `11aab38a` | Defer byte-backed function labels | esbuild admission -97.94% allocs; sqlite admission -5.64% time; Ruby admission -7.53% time |
| `ba3a791d` | Fuse SIMD discovery into the required-feature body walk | required-feature geomean -4.13% time and -91.74% allocs; full-compile alloc count -21% to -25% on wasm3/SQLite/Ruby |
| `71e4ac7a` | Collect segment lifecycle indexes during feature discovery | interleaved full compile: wasm3 -13.26%, SQLite -13.28%, Ruby -15.11%, esbuild -8.47% |
| `81edd4ab` | Reuse Railshot loop-hoist scans for loop facts | esbuild native lowering -7.04%, full compile -4.61%; native code bytes exactly unchanged |

### Cumulative allocation outcome

Allocation counters are deterministic and can be compared across the campaign
without relying on wall-clock frequency stability:

| Full compile | Baseline allocs/op | Final allocs/op | Change | Baseline B/op | Final B/op | Change |
|---|---:|---:|---:|---:|---:|---:|
| wasm3 | 12.86k | 8.848k | -31.2% | 1008.4 KiB | 926.3 KiB | -8.1% |
| SQLite | 38.56k | 24.17k | -37.3% | 3.351 MiB | 3.077 MiB | -8.2% |
| Ruby | 270.3k | 147.7k | -45.4% | 19.38 MiB | 17.17 MiB | -11.4% |
| esbuild | 512.7k | 156.9k | -69.4% | 91.47 MiB | 86.35 MiB | -5.6% |

For esbuild frontend admission alone, the measured result moved from about
339.7k allocations / 4.502 MiB to 173 allocations / 9.633 KiB. Remaining
sampled allocations in that focused profile came from paused prerequisite
setup rather than the support pass.

## Rejected experiments

- **Byte-context struct through the opcode decoder:** removed function-label
  allocations but regressed broad full compile by roughly 5-6%; reverted.
- **Fixed-array GP pin pool:** interleaved geomean was -0.11% time, -0.17% B/op,
  and -5.28% allocs, below the campaign acceptance threshold; reverted.
- **General feature scanning for every initializer expression:** removed
  allocation but increased required-feature latency; reverted and replaced by
  the specialized non-code SIMD scan.

The ignored `.perf/wasm3/` lab contains each hypothesis, base/candidate SHA,
commands, raw benchmark output, benchstat output, profiles, and verdict.

## Code size, execution, instantiation, GC, and parallelism

Railshot code-size metrics were identical before and after the accepted loop
scan fusion: wasm3 1.044 MiB, SQLite 4.485 MiB, Ruby 51.05 MiB, and esbuild
30.97 MiB.

No accepted change altered generated instruction selection or steady-state
runtime semantics. Final focused hot-path medians included:

- instantiate small scalar: about 1.23 us, 1.312 KiB, 7 allocations;
- call overhead: about 12.5 ns, zero allocations;
- host roundtrip: about 169 ns, zero allocations;
- global/local/memory micro-operations: roughly 54-59 ns, zero allocations;
- GC struct packed set/get: about 291-295 ns, zero allocations;
- collector defined/canonical ref tests: about 31 ns / 26 ns, zero allocations;
- remembered tiny array write: about 31 ns, zero allocations.

Parallel compilation remains a speed/memory tradeoff. In final esbuild
multi-module samples, `p1` was about 790 ms with about 90.6 MB allocated, while
automatic workers were about 454 ms with roughly 160-165 MB allocated: around
1.7x throughput at substantially higher transient allocation. The default
worker policy was not changed.

Five separate esbuild cold-compile process samples showed no peak-RSS
regression: baseline median approximately 102,936 KiB and final median
approximately 102,492 KiB (about -0.4%, effectively noise). The accepted
summaries are ephemeral and do not add retained `Compiled` or instance state.
Lifecycle/root-release tests remained green.

## Correctness and CI gates

Passed with pinned tools where applicable:

- `make lint`
- `make test`
- `make test-corpus` (explicit and guard-page corpus)
- `make simd` (470 modules, 24,325 assertions)
- `make test-guard`
- `make card-fragments`

The long all-subcase GC benchmark sweep exceeded the 11-minute test timeout;
a shorter focused GC-helper sweep passed. This was a benchmark-duration issue,
not a correctness failure.

Not run:

- pinned MoonBit JSON smoke/benchmark: required compiler version unavailable;
- Starshine smoke/benchmark: artifact not configured.

## Result

The campaign retained six production optimizations and three attribution
commits. The largest repeatable results are 8-15% cold full-compile wins from
removing a redundant segment-state pass, a 7% Railshot lowering win on esbuild,
and 31-69% lower full-compile allocation counts on the large fixtures, with no
native code-size, peak-RSS, execution-allocation, or lifecycle regression.
