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

Wago's `callIndirect` already does table bounds, null, canonical type, home/context, and wrapper/internal dispatch; immutable tables have separate type-check and monomorphic shortcuts. The exported-table property blocks Wago's immutable hint on all eight candidates, correctly allowing host writes. A one-entry mutable cache needs an identity guard (canonical descriptor, including cross-instance ownership), a safe miss path, per-instance cache state, and GC/reference lifetime handling. This dynamic probe identifies FastTree as the only useful candidate in these small command runs, but no stable runtime cache slot or invalidation/lifetime protocol exists. A compile-time shortcut from the observed table index would be unsound after a host table write. The [V8 article](https://v8.dev/blog/wasm-speculative-optimizations) concerns a feedback-driven optimizing tier with deoptimization and is not evidence of a Wago gain; #922 excludes that machinery. No production cache was added.

The focused static test passes. Production native bytes and compile resources are identical to base. A short unchanged-code indirect-call control (`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkInvokeTable0IndirectFixed$' -benchtime=50ms -count=2 -benchmem`) gives base `14.61/14.73 ns/op`, head `14.42/14.76 ns/op`, all `0 B/op`, `0 allocs/op`; this is an existing fixed-table path, not an admitted mutable site. The spread is noise. No guarded cache or real-workload speedup measurement exists.

Recommendation: leave draft. This is now a reproducible candidate profile, not a cache implementation or speedup claim. If pursuing a guarded prototype, start with FastTree and keep table bounds/null/type/miss semantics and host mutation tests; benchmark a larger input to see whether 1,131 calls are material in total execution time. Do not allocate per-site cache state for the six-call sed polymorphic site.
