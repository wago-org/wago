# Separate age2 sampled CPU attribution

Same existing measured binary, runtime5dfe7d3c and SHA in provenance; no rebuild or weighted changes. Intel8573C, explicit total young2 MiB/two8 MiB old spaces. Each profile128 warmups+512 following calls, identical baseline/age2 workloads. All four runs pass checksum/allocation/real-GC checks. These are sampled attribution runs, NOT benchmark timings; do not compare their elapsed durations as acceptance results.

| Workload/mode | Sampled CPU | External/JIT flat | Collector cumulative | layoutAt cumulative | eachRef cumulative |
|---|---:|---:|---:|---:|---:|
| Struct promote-all |5.57s|69.30%|26.57% (1.48s)|5.39%|7.36%|
| Struct age2 |5.97s|63.65%|31.99% (1.91s)|8.71%|11.56%|
| Array promote-all |12.95s|53.36%|41.70% (5.40s)|9.65%|14.21%|
| Array age2 |25.02s|34.93%|61.83% (15.47s)|20.90%|22.18%|

Array age2 concentrates sampled work in collectAge2, repeated layout resolution and edge traversal, while eliminating majors does not remove the larger minor-path cost. This is consistent with the observed increase in minor frequency, but does not isolate frequency from per-collection overhead. The array age2 layoutAt flat share is17.03%; collectAge2 itself18.47%. Memory clearing and memmove each2.36% flat are not the dominant visible costs. A focused next experiment should reduce repeated checked layout/edge resolution inside the collector, or alter survivor geometry, without weakening graph validation/transactional failure semantics.

Cumulative functions overlap and must not be summed. Profiles include initialization, warmup, invocation and result emission. Go profiling cannot reliably unwind or identify individual JIT instructions; runtime._ExternalCode is not a precise per-op attribution. Samples may be biased by safepoints and shared-host scheduling. No speedup, V8 parity, or default-policy recommendation is implied; age2 remains experimentally disabled. Raw pprof, flat/cumulative text, complete probe outputs and binary provenance are preserved separately from benchmark evidence.
