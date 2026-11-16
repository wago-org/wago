# Original ABI3 CPU attribution

The original best-pipeline binary was SHA-verified against the gate evidence. This separate profiling run used 2 MiB Eden, two 8 MiB old semispaces, 128 warmup calls and 512 subsequent calls. Both probes passed checksum, exact object-allocation and actual-collection checks. Profiled elapsed times are not benchmark results.

| Case | Sampled CPU | External code flat | Direct collection cumulative | Other observed costs |
|---|---:|---:|---:|---|
| Struct | 3.19 s | 46.39% | 51.41% | Minor 47.96%; full 3.45%; stride 10.34% flat; header u32 reads 10.03% flat; memmove 4.39% flat |
| Bulk copy | 0.80 s | 26.25% | 2.50% | Host loop 66.25% cumulative; array helper dispatcher 37.50%; ArrayCopy 20.00%; native resume 12.50% |

Cumulative percentages overlap and must not be added. The struct profile supports investigating conventional collector traversal/header work as a substantial cost, not attributing its entire gap to code generation. The copy profile supports prioritizing helper/native-transition elimination for that kernel rather than tuning collection. These are hypotheses about where to optimize, not measured speedups.

Go sampling cannot precisely attribute or unwind generated native instructions here: `runtime._ExternalCode` is not a named JIT operation. The profiles include setup, compilation, warmup, host GC and output work as well as invocations. Copy has only 80 nominal 10 ms samples, so small percentages are coarse. No V8 profile, equal-memory comparison or collector-efficiency proof is supplied.

The first report attempt failed because Go's default build cache was read-only; rerunning with the existing writable GOCACHE/GOPATH produced both profiles and reports without rebuilding or changing the candidate. The initial successful probe's record is retained separately. The current reports correspond to the final `profile-run.json` records.
