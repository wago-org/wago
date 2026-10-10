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
