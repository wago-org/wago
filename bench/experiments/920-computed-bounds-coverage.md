# #920: computed memory32 bounds keys — narrow phase-0 no-match

PR production baseline: branch parent `8097344a63a79e96230e09c883b2138232151370`. The diff from that parent adds only the survey test and this record; it changes no production source. Historical timing samples below used `209e448c392510a0325d5b282a0d86a776fb379c` as their recorded baseline.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestComputedBoundsSourceUpperBound$' -count=1 -v` scans six decoded real modules. The admitted source shape is exactly `local.get; i32.const; i32.add; scalar load`, keyed by local index, added constant, memory index 0, load opcode/width, and static memarg offset. Prior keys are conservatively killed on writes, calls, SIMD/atomic/bulk instructions, local updates, or control. This is a source-level **upper bound for that one form**, not a backend count after canonical-i32, folded addresses, hoisting, and existing certificate hits.

| Module | Scalar loads | This computed form | Repeated exact keys |
| --- | ---: | ---: | ---: |
| SQLite | 48,675 | 798 | 0 |
| QuickJS | 20,343 | 277 | 0 |
| jq | 21,482 | 307 | 0 |
| PHP | 869,542 | 2,072 | 0 |
| Lua | 9,137 | 318 | 0 |
| yyjson | 2,736 | 112 | 0 |

The [Leaps and Bounds paper](https://www.pure.ed.ac.uk/ws/portalfiles/portal/296056934/Leaps_and_bounds_SZEWCZYK_DOA02092022_AFV.pdf) studies bounds-check costs in several runtimes; its effects do not establish a Wago gain. Wago's existing `memAddr` certificates only key stable local/global carriers. Exact computed source keys here would need versioned operand identity and trap proof; a source-level match alone cannot suppress a check. #818 already rejected a larger certificate table. No extra keys were added for the no-match class.

The focused test passes. Production compiler source is unchanged against the actual PR parent. Native bytes and compile allocations against that parent were not measured. Historical short samples for `BenchmarkStagedMultiMemoryLoads/load0` (`-benchtime=50ms -count=2 -benchmem`) give historical baseline `209e448c` `128.1/130.3 ns/op`, historical experiment `134.4/129.1 ns/op`, all `0 B/op`, `0 allocs/op`; the exact experiment binary identity is not retained in this record. Production source differs between `209e448c` and the actual PR parent, so these samples are not an unchanged-code control for this PR. The samples are noisy and do not establish a PR speedup or a candidate result. Actual candidate execution frequency, code-size change, and memory costs cannot be measured without a selected implementation. Nested arithmetic and repeated compiler-derived expression identities remain outside the survey, so it does not close every form contemplated by #920.

Recommendation: the completed static survey is ready for review. Do not add a production key for this no-match source form. Broader backend-stage admission would need separate measurement before implementation.

A followup broadened the focused scan to the commutative, immediately reversed `i32.const; local.get; i32.add; scalar load` shape. Across the same six applications it found **zero reversed forms**, and the forward-form and repeated-key counts stayed exactly as above. The same focused test passes with `-count=2`. This rules out only that additional syntactic shape; nested arithmetic and backend-derived identities are still unmeasured. No production bounds key or result change follows.

Review correction: the scan now retains every exact key until the next conservative barrier. A focused A/B/A regression fails with the previous last-key-only counter and passes with the corrected window. Local writes and stores still clear all keys. Rechecking all six modules leaves every count above unchanged. This remains survey evidence; no production bounds-proof key is added.

Control-barrier correction: the scan clears prior keys at `call_ref`, `return_call_ref`, `try_table`, `br_on_null`, and `br_on_non_null`. The entire GC prefix is a conservative barrier, including `br_on_cast` and `br_on_cast_fail`. Existing store, local-write, bulk, SIMD, and atomic barriers remain. Focused instruction-stream regressions keep a repeat across straight-line integer work and reject repeats across all six call forms, current EH boundaries, and reference branches. The classifier rejects legacy `try`, `catch`, `rethrow`, `delegate`, and `catch_all` before any survey result is returned. Rechecking all six applications leaves every count in the table unchanged. No production bounds-proof key or timing result is added.
