# TinyGo small operand chunks

Experimental TinyGo-only geometric chunk cap: 256 nodes instead of 8192. With current 56-byte nodes, grown backing arrays are at most 14 KiB instead of 448 KiB. Production first-chunk estimates already cap at 256; explicit oversized test/helper initial capacities are not changed.

Hypothesis: TinyGo conservative marking resolves many interior pointers in operand arrays by scanning toward the object's terminal metadata. Existing exact-binary pcre2 profiling places 192/761 flat samples in that object-head search, but does not identify the pointed-to allocation. Smaller stable-pointer chunks may bound those scans without changing the collector, node layout, emission order, or ownership. More chunks, allocation headers, slice growth and transitions may outweigh any benefit.

Ordinary Go retains 8192. Existing per-worker 1 MiB retention, function reset, explicit clear, mapped-arena behavior and cold roots remain unchanged. No promise of speedup. Qualify actual TinyGo cross-chunk/reset/recycle tests, exact145 native code/oracles, compilation latency, external absolute RSS and execution before adoption. Prior handle packing was negative under ordinary Go; this experiment retains pointers and specifically tests a TinyGo conservative-marking hypothesis.

## Correctness qualification

Ordinary Go full AMD64 package passed (8.646s), including ordinary-Go8192 assertion, cross-chunk links/reset/erase and new standalone empty-arena recycling; initial focused arena tests passed0.009s. Actual TinyGo tasks/conservative/opt2/256KB isolated test result is retained in tinygo-focused.log. Only stack_chunk_* test files were retained for that invocation; all production source was unchanged, every other test path is listed in tinygo-exclusions.json, and the runner restores those files afterward. This is not a full TinyGo backend suite: prior full-package TinyGo compilation fails on pre-existing compile_test.go lines111,125,130 indexing zero-sized scratch arrays. No assertions were weakened.
