# Wago compilation memory: final selected candidate

## Result

The 50% peak-memory target was not reached. The selected candidate reduces the geometric mean of cold process peak RSS by 4.39% against measurement-time main and 4.23% against the original pinned main. Total allocated Go bytes fall 19.79% geometrically. Esbuild, the largest pinned input, falls from 139.64 MB to 111.73 MB peak RSS against measurement-time main (19.99%), or 135.38 MB to 111.73 MB against original main (17.47%).

Esbuild allocation traffic is 161.23 MB → 90.82 MB (43.67% lower), while post-GC retained heap is 92.69 MB → 87.10 MB (6.04% lower). These are separate quantities. Small inputs are not uniformly better: tiny has a measured 2.11% peak increase against measurement-time main, despite lower allocation traffic. No universal per-module reduction is claimed.

## Revisions and method

- Candidate: `c6f19404b1cac13618c9e0fc2bfb36d4de68f246`
- Measurement-time-main baseline: `bdf04d32ee8079eb7e21a3975aa11261b175592b`
- Original target baseline: `b70db34c0184bfab4dcc61e59f4f3cd75796d8ff`
- Linux/amd64, AMD EPYC 9V74; official Go 1.27.1; CGO_ENABLED=0; -buildvcs=false; GOAMD64=v1
- GOMAXPROCS=1, taskset CPU 0; 22 pinned Wasm inputs; five alternating fresh-process pairs per comparison
- Identical harness source SHA-256:48ef47df4638638a00f978532777a07f7db172ffadeda20da9ec3616c5499966
- Both comparisons contain 220 raw samples, 440 total. All native code sizes and SHA-256 hashes match strictly within each input across both revisions and every repetition
- RSS is the Linux kernel process high-water mark through compilation, including startup/input. It is captured before diagnostic post-compilation GC and hashing
- Allocated bytes are cumulative Go allocation traffic during Compile. Retained heap is a post-GC observation with the compiled result alive
- Compile takes ownership of input. The harness keeps a read-only source alias reachable through close, so the source-sized after-close baseline is not an engine leak
- The launcher uses a lightweight non-tail-exec shell. Three calibration samples matched direct-shell startup/peak RSS, avoiding an inherited Python RSS floor. No unrelated high-water values are subtracted

## Measurement-time-main comparison

MB means 1,000,000 bytes. Values are medians. The JSON summary preserves all ranges and both baseline comparisons.

| Input | Baseline peak MB | Candidate peak MB | Peak change | Allocation change | Retained change |
|---|---:|---:|---:|---:|---:|
| tiny | 6.603 | 6.742 | +2.11% | -1.94% | +0.15% |
| fib_rec | 6.472 | 6.480 | +0.13% | -1.19% | -0.15% |
| many_funcs | 6.472 | 6.480 | +0.13% | -0.27% | -0.04% |
| fannkuch | 6.603 | 6.349 | -3.85% | -0.51% | +0.31% |
| matmul | 6.472 | 6.218 | -3.92% | -38.74% | -0.26% |
| sha256 | 6.603 | 6.349 | -3.85% | -0.57% | -0.24% |
| raytrace | 7.127 | 6.873 | -3.56% | -26.65% | +0.27% |
| json-as | 6.865 | 6.742 | -1.79% | -19.11% | +0.05% |
| json-as-simd | 6.865 | 6.742 | -1.79% | -18.12% | -0.02% |
| blake-as-simd | 7.389 | 7.135 | -3.44% | -31.56% | -0.13% |
| utf-as-simd | 7.389 | 7.135 | -3.44% | -18.01% | +0.06% |
| polybench-cholesky | 6.996 | 6.873 | -1.76% | -0.44% | -0.23% |
| polybench-correlation | 6.996 | 6.873 | -1.76% | -0.44% | -0.05% |
| polybench-gemm | 6.996 | 6.873 | -1.76% | -0.47% | -0.23% |
| coremark | 7.258 | 7.004 | -3.50% | -46.01% | -0.04% |
| blake3 | 7.389 | 7.266 | -1.66% | -0.15% | -0.03% |
| yyjson | 11.059 | 9.757 | -11.78% | -23.13% | +0.03% |
| lodepng | 7.782 | 7.135 | -8.32% | -59.91% | -0.01% |
| pcre2 | 13.025 | 11.461 | -12.01% | -24.23% | +0.01% |
| quickjs-script | 12.501 | 11.985 | -4.13% | -18.75% | +0.00% |
| sqlite3-query | 16.564 | 15.917 | -3.91% | -13.77% | +0.00% |
| esbuild-minify | 139.641 | 111.731 | -19.99% | -43.67% | -6.04% |

Esbuild RSS ranges: baseline 135.315–140.689 MB; candidate 110.944–111.862 MB. Baseline run-to-run spread is meaningful; do not present a fastest sample as the result.

## Attribution and design

1. Operand arenas recycle only at proven-empty ordinary reader boundaries, including zero-height nested controls. Inline readers and profiling are excluded; live control/register owners prevent reuse; current and cached transient handles are cleared
2. Exact data-section capacity removes geometric growth buffers, while keeping the original conservative admission budget. Impossible counts fail before allocation. Padded malformed input may allocate the full budget-admitted vector before an early syntax error, a documented bounded tradeoff
3. Active offset expressions now occupy their existing byte-backed public Data field rather than a duplicate transient directory. Current caller-replaced offsets are validated rather than stale positional summaries
4. Large simple immutable execution snapshots use 16-byte pointer-free data records plus owned payload. Public DataInit metadata, codec bytes, and the 784-byte amd64 Compiled footprint remain unchanged

Separate phase diagnostics on an earlier checkpoint candidate showed decoder peak RSS 73.3 → 43.3 MB and backend-end peak 120.9 → 97.0 MB after exact capacities. Publication then became the high-water stage. These phase reads did not force GC or modify GC policy, but are diagnostic instrumentation, not final latency samples

Esbuild has 100,000 active data segments and 5,546,669 payload bytes. Its original public and frozen 72-byte descriptors alone total 14.4 MB. Compact frozen records save approximately 5.6 MB; only one adjacent segment pair is contiguous, so simple coalescing is ineffective. Native output (45.07 MB), source bytes (20.68 MB), and required metadata remain substantial retained costs. No exact decomposition of every byte of process peak is claimed

Rejected: 64-node checkpoints had no useful RSS gain and some regressions; early writable native mapping mostly shifted heap accounting without lowering RSS; the broader index-handle allocator was rejected by the integration owner after final latency comparison. None is in the selected candidate

## Verification and limits

- Full wasm decoder tests pass, including exact capacity, truncation, malformed encoding, unchanged budget, mixed offsets and current-view validation
- Focused ordinary and register-checker snapshot/quota/mutation/codec tests pass; independent read-only review found no remaining blocker
- Memory branch pure-Go runtime CLI cross-builds pass for linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 and windows/arm64. These are builds, not native execution on those targets
- Selected TinyGo 0.42.0 optimized minimal release is 2,717,456 bytes after CI-style GNU stripping, below the unchanged 2,720,000-byte budget. Three fresh fib(20) runs return 6765
- Exact selected ordinary official v1 TestSpecExec matches baseline bdf04d32: 55/57 applicable files fully passing, 16,585 assertions passed, zero failed, 1,598 skipped. Imports/names remain partial due to duplicate spectest.print_i32 imports; this is not full conformance
- Broader Data/Snapshot/Artifact/Codec tests encountered official Release 3 fixture-conversion blockers: WABT cannot parse all GC fixtures and the pinned official interpreter is unavailable. No full v3 pass is claimed
- Final execution-speed measurements, validation and limitations are described in README.md and validation.txt

## Evidence

- final-memory-vs-latest.jsonl and final-memory-vs-original.jsonl: complete raw samples
- final-memory-summary.json: medians, ranges and ratios for every metric
- final-corpus.json: exact paths, byte sizes and input hashes
- final-memory-binaries.sha256: tested executable identities
- Phase logs, tests and prior rejected experiments are retained in the recovery archive
