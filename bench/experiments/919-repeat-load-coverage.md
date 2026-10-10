# #919: repeated linear-memory loads — phase-0 upper bound

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestRepeatedLinearLoadUpperBound$' -count=1 -v` scans six decoded real modules. It counts identical scalar load opcodes with the same preceding `local.get`, memory index, and immediate offset in one straight-line run. It invalidates the prior candidate on stores, calls, bulk/atomic/SIMD operations, local writes, and control transitions. This is deliberately a **source-level upper bound**: it does not establish that both addresses remain identical after local pins, that the first load survives operand folding, or that a retained register avoids spills.

| Module | Scalar loads | Strict repeated-load upper bound | Share |
| --- | ---: | ---: | ---: |
| SQLite | 48,675 | 0 | 0% |
| QuickJS | 20,343 | 0 | 0% |
| jq | 21,482 | 0 | 0% |
| PHP | 869,542 | 4,242 | 0.49% |
| Lua | 9,137 | 0 | 0% |
| yyjson | 2,736 | 0 | 0% |

The [DITWO paper](https://monkbai.github.io/files/issta23-ditwo.pdf) studies missed Wasm optimization opportunities in a different optimizer; its reported performance estimates are not evidence of a Wago gain. Wago's existing [store-to-load forwarding](https://github.com/wago-org/wago/blob/209e448c392510a0325d5b282a0d86a776fb379c/src/core/compiler/backend/railshot/amd64/memory.go#L1088-L1207) has a separate narrow window. This test leaves it unchanged.

The PHP candidates require a backend-stage count after pins/folding, liveness and spill accounting, and a dynamic hotness sample before a cache can be judged. It is possible that none survive or that a cache increases traffic. A source-only cache would be unsound across aliases, traps, branches, calls, memory.grow, shared/atomic access, or local mutation. No production cache was introduced; the result is inconclusive for PHP and no-match for the other five modules.

The focused test passes. Production compilation and native code are unchanged. Short unchanged-code control benches (`-benchtime=50ms -count=2 -benchmem`) show no attributable effect: `BenchmarkStagedMultiMemoryLoads/load0` base `125.3/134.3 ns`, head `126.4/126.2 ns`; load1 base `134.9/136.1`, head `139.1/138.5 ns`, all `0 B/op`, `0 allocs/op`. `BenchmarkCallPresenceCompile` base `62177/37829 ns`, head `51313/43797 ns`, both `96 allocs/op` and about `19.45 KiB/op`; samples are noisy. Native bytes are identical; peak memory and PHP dynamic execution were not measured.

Recommendation: keep draft. Do not enable a region cache without measuring actual backend survivors and a hot real site; the available evidence does not meet the issue's acceptance threshold.

## Real PHP execution followup

A disposable WABT rewrite, `bench/experiments/919-profile-repeat-loads.py`, adds a per-site counter before each strict source candidate. It found **exactly 4,242 sites**, matching the binary-decoder test. The opt-in bench-suite test runs the original PHP command against the corpus oracle, then runs two fresh instrumented instances and requires identical results, stdout, stderr, output files, and site counts. Two complete passes were identical:

```sh
cd bench
WAGO_919_PROFILE=1 GOCACHE=/tmp/wago-go-cache go test ./suite -run '^TestRepeatedLinearLoadDynamicPHP$' -count=2 -v -timeout=120s -args -wago.corpus=all
```

On that real PHP input, **211/4,242 candidate sites** execute, across 39 source functions, for **56,958 candidate-load executions**. Site 3016 in function 7585 (`local.get 3; i32.load offset=24`) executes **6,254** times. The repeated expression appears in a loop whose body also stores to nearby stack-frame addresses. The program outputs match the unmodified module, and the counts were stable across runs. This is a useful candidate-hotness result, not a count of redundant native loads. Instrumentation grows Wasm from 13,622,978 to 13,760,881 bytes and compiled native code from 30,164,957 to 31,329,073 bytes; its timing cannot be used for a speedup comparison.

The backend defers loads as `stMemRef` and may fold them into later consumers. A sound cached value must preserve the first load's trap point and handle intervening stores/aliases, local changes, calls, memory growth, shared memory, branch edges, and register spills. The new dynamic result prioritizes function 7585 for a **post-lowering** survivor check and a tightly bounded duplicate-pair prototype. Production code remains identical to base, so prior unchanged-code control benchmarks remain the only baseline/head timing comparison. Keep the PR draft: no retained-value cache or performance improvement has been established.
