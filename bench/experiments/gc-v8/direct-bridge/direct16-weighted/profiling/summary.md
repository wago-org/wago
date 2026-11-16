# Separate direct16 sampled attribution

Existing weighted-scalar timing binary, runtime a2e066d2, no rebuild. Two fresh
processes, struct and array allocation workloads, each 128 warmup + 512 following
calls, nursery 2 MiB and two 8 MiB old semispaces. Direct mode, age2/compact and
diagnostics OFF. Both runs passed exact allocation, checksum and real-GC checks.
Binary hash and raw profiles, run output, flat/cumulative reports are retained.

| Sample attribution | Struct | Array |
|---|---:|---:|
| Total sampled CPU seconds | 3.52 | 8.09 |
| External code | 46.31% | 44.87% |
| Collector collect cumulative | 49.72% | 51.92% |
| retireNurseryHeaders cumulative | 27.27% | 19.65% |
| stride cumulative | 16.19% | 19.90% |
| eachRef cumulative | 12.78% | 15.57% |
| memmove flat | 1.14% | 2.84% |

Cumulative costs overlap and must not be summed. Nursery header retirement and
repeated stride/header reads are substantial sampled costs, making retirement
and checked layout reuse worthwhile hypotheses for separate experiments. This
profile does not establish an improvement or justify removing validation.

These are whole-process profiles, including warmup/setup/invocation/reporting,
not matched performance timings. Go cannot reliably attribute individual JIT
instructions through ExternalCode. No per-op JIT or V8 speed claim follows.
