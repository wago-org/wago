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

The PHP candidates require a backend-stage count after pins/folding, liveness and spill accounting, and a dynamic hotness sample before a cache can be judged. It is possible that none survive or that a cache increases traffic. A source-only cache would be unsound across aliases, traps, branches, calls, memory.grow, shared/atomic access, or local mutation. The later followup below adds a narrow opt-in backend prototype; this phase-0 observation remains the baseline admission result.

The focused test passes. Production compilation and native code are unchanged. Short unchanged-code control benches (`-benchtime=50ms -count=2 -benchmem`) show no attributable effect: `BenchmarkStagedMultiMemoryLoads/load0` base `125.3/134.3 ns`, head `126.4/126.2 ns`; load1 base `134.9/136.1`, head `139.1/138.5 ns`, all `0 B/op`, `0 allocs/op`. `BenchmarkCallPresenceCompile` base `62177/37829 ns`, head `51313/43797 ns`, both `96 allocs/op` and about `19.45 KiB/op`; samples are noisy. Native bytes are identical; peak memory and PHP dynamic execution were not measured.

Phase-0 recommendation was to keep draft and measure backend survivors and a hot real site; the followups below do both for a narrow adjacent-pair subset.

## Real PHP execution followup

A disposable WABT rewrite, `bench/experiments/919-profile-repeat-loads.py`, adds a per-site counter before each strict source candidate. It found **exactly 4,242 sites**, matching the binary-decoder test. The opt-in bench-suite test runs the original PHP command against the corpus oracle, then runs two fresh instrumented instances and requires identical results, stdout, stderr, output files, and site counts. Two complete passes were identical:

```sh
cd bench
WAGO_919_PROFILE=1 GOCACHE=/tmp/wago-go-cache go test ./suite -run '^TestRepeatedLinearLoadDynamicPHP$' -count=2 -v -timeout=120s -args -wago.corpus=all
```

On that real PHP input, **211/4,242 candidate sites** execute, across 39 source functions, for **56,958 candidate-load executions**. Site 3016 in function 7585 (`local.get 3; i32.load offset=24`) executes **6,254** times. The repeated expression appears in a loop whose body also stores to nearby stack-frame addresses. The program outputs match the unmodified module, and the counts were stable across runs. This is a useful candidate-hotness result, not a count of redundant native loads. Instrumentation grows Wasm from 13,622,978 to 13,760,881 bytes and compiled native code from 30,164,957 to 31,329,073 bytes; its timing cannot be used for a speedup comparison.

The backend defers loads as `stMemRef` and may fold them into later consumers. A sound cached value must preserve the first load's trap point and handle intervening stores/aliases, local changes, calls, memory growth, shared memory, branch edges, and register spills. The new dynamic result prioritizes function 7585 for a **post-lowering** survivor check and a tightly bounded duplicate-pair prototype. The opt-in prototype below now changes code for a narrow subset. The original unchanged-code control remains valid only for the flag-off path.

## Narrow opt-in backend prototype and A/B

`WAGO_AMD64_EXPERIMENT_REPEAT_LOAD=1` enables only an **adjacent pair** whose
first load remains a deferred memory reference, both effective addresses
borrow the same pinned local register, memarg/width/sign match, and memory is
unshared memory32 with explicit bounds. Any intervening store/call would force
the first deferred load and exclude the pair. The second bounds check remains;
the first value is materialized in source order, copied to an independently
owned register, and left for both consumers. No region cache, SSA, added
function-state allocation, or default-path change is introduced.

`TestAdjacentRepeatLoadExec` passes with the flag off and on for duplicate
loads, an intervening store, local mutation, and OOB trapping. The full AMD64
backend package passes with the flag on. The synthetic case and PHP command
also pass under `-tags=wago_regalloccheck`. The real `php-buckets` corpus command
passes its pinned output oracle with the flag on. Diagnostics from an
original-module standalone backend compile under `-tags=wago_codegenstats`
show **942 selected pairs in 530 PHP functions** and the resource counts below.

After restoring the exact
spec-v3 submodule revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`
and using repository-pinned WABT 1.0.41, the full opt-in
`go test ./src/wago ./src/core/compiler/backend/railshot/amd64 -count=1
-timeout=180s` passed both packages (14.424s and 3.335s).

| Mode | Native B | Function body B | Spills | Reloads |
| --- | ---: | ---: | ---: | ---: |
| Default | 30,153,325 | 30,071,824 | 414 | 62,213 |
| Opt-in | 30,151,517 | 30,069,852 | 414 | 62,213 |

Those are static emission counts. The 942 selected sites are a subset of the
4,242 source candidates, not proof that all 942 execute in the PHP command.
The dynamic source probe above identifies hotness but inserts counters that
force the first deferred load, so it cannot measure this optimization's actual
runtime hit count.

Run the first two commands from the repository root and the remaining two from `bench`:

```sh
WAGO_AMD64_EXPERIMENT_REPEAT_LOAD=1 GOCACHE=/tmp/wago-go-cache go test ./src/core/compiler/backend/railshot/amd64 -count=1 -timeout=120s
WAGO_AMD64_EXPERIMENT_REPEAT_LOAD=1 GOCACHE=/tmp/wago-go-cache go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestRepeatLinearLoadPHPCodegen$' -count=2 -v
WAGO_AMD64_EXPERIMENT_REPEAT_LOAD=1 WAGO_APP_CORPUS_REFERENCE=wago-only GOCACHE=/tmp/wago-go-cache go test ./suite -run '^TestApplicationCorpusRuns/php-buckets/wago$' -count=1 -v -timeout=120s -args -wago.corpus=all
GOCACHE=/tmp/wago-go-cache go test ./suite -run '^$' -bench '^BenchmarkRepeatLoadPHP(Command|Compile)$' -benchtime=10x -count=3 -benchmem -cpu=1 -timeout=120s -args -wago.corpus=all
```

Run the last two benchmark patterns as **separate processes** with the flag
unset and set; compile samples used `-benchtime=2x -count=2`. On this Linux
AMD64 Ryzen 7 8845HS, PHP compile was default **637.58/643.18 ms** versus
opt-in **640.70/642.45 ms**, identical **116,537,544 B/op and 35,766
allocs/op**. Ten-command PHP execution samples were default **4.84/4.77/5.16
ms** versus opt-in **5.58/5.18/5.29 ms**. An immediately repeated pair gave
default **5.33/5.25 ms** versus opt-in **4.67/4.68 ms**. All execution samples
reported about **77.6 KiB/op and 961 allocs/op**. This crossover is noise or
workload sensitivity, not a credible speedup. The compiler emits about 1.8
KiB less native code with unchanged aggregate spill/reload counts, but there
is no meaningful resource or end-to-end win yet.

A later short crossover check on the same real PHP command (
`-benchtime=10x -count=3 -benchmem -cpu=1`, separate processes) gave
default **5.725/5.146/5.157 ms/op** and opt-in
**5.164/5.409/5.214 ms/op**, again **961 allocs/op** on both sides.
These samples overlap; the draft recommendation is unchanged.

**Recommendation:** keep the flag off and the PR draft. The prototype proves
one bounded safe lowering and supplies A/B controls; it does not meet the
issue's benefit threshold. A next experiment should correlate *selected*
backend sites with dynamic execution and measure a hot isolated function before
widening the admitted pattern.
