# #922: mutable-table indirect-call cache — real-command target profile

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestMutableIndirect' -count=1 -v` decodes six large applications and all 121 shipped workload Wasm files. It counts `call_indirect`/`call_ref` sites and marks a module potentially mutable if it imports or exports a table or contains table set/grow/fill/copy/init. This is deliberately a **module-level upper bound**, not a count of mutable sites after Wago's immutable-table analysis.

SQLite has 1,998 static indirect sites, QuickJS 723, jq 547, PHP 8,188, Lua 112, and yyjson 83; each has zero table mutation instructions and no imported/exported table, so these are not new mutable-table-cache coverage. Across the 121-file corpus, eight modules have potentially mutable tables and indirect calls, totaling **191 static sites**: FastTree 25, GNU seq 14, GNU tr 12, sed 35, LCS 6, Needleman–Wunsch 35, Smith–Waterman 35, seqtk 29.

## Dynamic real-command probe

`bench/experiments/922-profile-indirect.py` inserts disposable per-site counters into the eight corpus Wasm modules with WABT. The opt-in Go test runs each original command against its existing oracle, then runs the instrumented module twice with fresh instances and requires identical results, stdout, stderr, output files, and counts. Run from `bench`:

```sh
WAGO_922_PROFILE=1 GOCACHE=/tmp/wago-go-cache go test ./suite -run '^TestMutableIndirectDynamicProfile$' -count=2 -v -timeout=120s -args -wago.corpus=all
```

Both complete test runs passed in 1.84 seconds total. None of these modules imports a table or executes guest table-mutation instructions; the test host never writes their exported table. Thus an observed table index identifies a stable target *for these runs*. This probe does not establish the distribution after a host table write or on larger inputs. Its added instructions make its timing unsuitable for a speedup claim.

| Command | Static sites | Active sites | Calls | Dynamic indices |
| --- | ---: | ---: | ---: | --- |
| LCS | 6 | 1 | 4 | one per active site |
| Needleman–Wunsch | 35 | 1 | 3 | one per active site |
| Smith–Waterman | 35 | 2 | 11 | one per active site |
| sed | 35 | 7 | 26 | six sites one; one site two (1/5 calls) |
| GNU seq | 14 | 1 | 1 | one |
| GNU tr | 12 | 1 | 1 | one |
| seqtk | 29 | 3 | 8 | one per active site |
| FastTree | 25 | 10 | 1,131 | one per active site |
| **Total** | **191** | **26** | **1,185** | **25 single-index sites, one two-index site** |

FastTree site 5 alone calls table index 1 **720** times; sites 0–2 call it 120 times each. The other seven commands together execute only 54 indirect calls. The full per-site bins, function indices, and original/instrumented Wasm sizes are printed by the test. Counters are per site, not shared; they show target concentration rather than a mutable cache speedup. The sole two-index site executes six times, too little to justify a two-entry strategy.

## Backend and decision

Wago's `callIndirect` already does table bounds, null, canonical type, home/context, and wrapper/internal dispatch; immutable tables have separate type-check and monomorphic shortcuts. The exported-table property blocks Wago's immutable hint on all eight candidates, correctly allowing host writes. The [V8 article](https://v8.dev/blog/wasm-speculative-optimizations) concerns a feedback-driven optimizing tier with deoptimization and is not evidence of a Wago gain; #922 excludes that machinery.

## Bounded guarded prototype

The second commit adds an **opt-in, single-site** AMD64 guard selected by `WAGO_AMD64_EXPERIMENT_INDIRECT_SITE=caller:bodyPC:target:table`. For FastTree the selector is `73:549:170:0`; the earlier WABT offset was one byte before Wago's body-PC coordinate. After the existing bounds, null, and type checks, the guard compares the table entry's canonical descriptor pointer with the selected local target's descriptor **in the current instance**. Equality takes the mixed register-ABI direct call; any mismatch, including a guest or host table write or a cross-instance funcref, takes the unchanged home-aware generic call. There is no cache slot, feedback table, allocation, or global speculative state. Admission is limited to an exact local target signature, the mixed register ABI, a valid descriptor displacement, and one designated site. With the environment variable absent, the default lowering is unchanged.

The synthetic test `TestGuardedMutableIndirectCall` checks the selected target, a different table index, guest `table.set` to another function and back, null, wrong signature, and out-of-bounds traps. `TestGuardedMutableIndirectCrossInstance` imports the producer's exported table into another instance and checks the cross-instance call before and after producer-side table mutation. Both pass under `wago_regalloccheck` with the guard selected; emitted code grows from **1,873 to 1,953 bytes** when selected. `TestGuardedFastTreeCommand` checks the pinned real command oracle; its emitted code grows from **722,041 to 722,121 bytes**, confirming admission. The same FastTree oracle passes under `wago_regalloccheck`. The full AMD64 backend package passes. The full `src/wago` package is blocked by the checkout's missing pinned `tests/conformance/spec-v3/test/core` fixture, unrelated to this guard. Direct host table writes through the shared-table handle are not exposed by Wago's public API; the test exercises the same shared backing table through a producer Wasm call.

Reproduce short end-to-end FastTree execution samples with `GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./bench/suite -run '^$' -bench '^BenchmarkGuardedFastTreeCommand$' -benchtime=100x -count=3 -benchmem -cpu=1 -args -wago.corpus=fasttree-phylogeny`, adding the selector environment variable for the variant. One baseline batch was **1.980–1.987 ms/op** versus guard **2.281–2.332 ms/op**; a repeated guard batch was **1.968–2.097 ms/op**. A 30-iteration batch crossed in the other direction. Every run was **852 allocs/op, about 73,528 B/op**. This host has variable load, and these samples do not establish a directional execution benefit. The guard likely adds too much overhead for this small command's 720 hot-site calls.

`BenchmarkCompileFull/fasttree-phylogeny`, 3 iterations × 2 samples, measured baseline **19.47/20.77 ms/op, 722,041 code bytes, 2,019,253/2,019,322 B/op, 1,399/1,400 allocs/op**; guard **18.47/18.23 ms/op, 722,121 code bytes, 2,019,525 B/op, 1,402 allocs/op**. Compile times overlap normal run noise; the small code and allocation increase is real. The earlier fixed-table 14.61/14.73 versus 14.42/14.76 ns/op control is not an admitted mutable site.

Recommendation: **keep draft and disabled**. The guard demonstrates a bounded safe fallback on tested local and cross-instance mutations, but no credible practical benefit. Before considering a general cache, profile hit/miss counts on a larger real input and audit concurrent host table writes through any future host mutation API. Do not allocate per-site cache state for the six-call sed polymorphic site.
