# Cold compilation memory

Build `go build -o compilemem ./scripts/compilemem` on Linux. In a fresh process,
run `GOMAXPROCS=1 taskset -c 0 ./compilemem path/to/module.wasm`. Pin the same
Go version, CPU, input hashes, options and build tags for both revisions. Run
alternating before/after pairs at least three times without concurrent workloads.

The command reports distinct quantities:

- `compile_peak_rss_bytes`: kernel process RSS high-water mark through compilation,
  including input and startup; this is physical process peak, not allocation traffic
- `startup_peak_rss_bytes`: corresponding high-water mark after loading input
- `allocated_bytes`: cumulative Go allocations during compilation
- `heap_after_compile_bytes`: current heap occupancy before a diagnostic collection
- `heap_retained_result_bytes`: live Go heap after GC while retaining compiled output
- `heap_after_close_bytes`: live Go heap after output close and GC

Retained heap includes the source input, intentionally kept reachable through
all measurements. `Compile` takes ownership of that input; the harness retains
a read-only alias so the after-close baseline still includes its bytes. Native mappings are included in RSS but not Go heap. The peak
is captured before the post-compilation diagnostic GCs. RSS has page granularity
and startup noise, so tiny modules may show no measurable peak change. Do not
subtract separate process high-water marks and label the result exact incremental
peak. Do not equate allocated bytes with peak memory, or retained heap with peak.

Compilation uses the existing public `Compile(nil, data)` path, including its
usual default explicit bounds checks, and does not run generated guest code.

Native code hashes are computed after compilation and retained-result heap
observations; hashing never contributes to reported compile RSS/allocation.
Input hashes and active data segment counts/payload/public descriptor sizes help
attribute results. The public descriptor count excludes its immutable snapshot
copy, decoder scratch, and allocator overhead; it is not whole-program memory.

For a manifest containing `id`, `path` and `sha256` entries (either an array or a
`corpus` array), use the same harness source against both pinned revisions:

```
python3 scripts/compilemem/compare.py --baseline /tmp/baseline-compilemem \
  --candidate /tmp/candidate-compilemem --manifest corpus-manifest.json \
  --output cold-memory.jsonl --repeat 5 --cpu 0
```

The runner verifies input hashes, alternates revision order, records every raw
sample immediately, and rejects mismatched native code bytes/hashes. Use it only
for ordinary deterministic corpus modules whose compilation contains no
process-specific native addresses. It launches through a lightweight shell that
forks the harness, preserves argument quoting and returns its exit status. This
avoids carrying Python's resident pre-exec RSS floor into tiny-module results.
Three calibration runs on this host matched direct shell invocation exactly:
4,636,672 startup bytes and 6,602,752 peak bytes for `tiny.wasm`; direct Python
launches instead reported an inherited 8,912,896-byte floor. Recheck this against
a direct invocation on a different host, and never subtract unrelated high-water
values to manufacture an incremental peak.
