# Experimental compiler checkpoint

Source and tests match qualified commit `72cfa414a9e0d981d670604c23b092cfa3f9ca5b`. Publication sanitizes machine-local paths in experimental evidence and scripts; the complete publication tree therefore differs. Original raw receipts remain retained privately.

Current 145-workload screen: cold compilation latency 22.77% lower than the Go baseline (40% target unmet); arithmetic-average absolute peak RSS 4.226 MiB; aggregate execution latency +2.58%, but 51 workloads exceed +5% in the three-trial screen. The per-workload execution gate is NOT met. These are experimental measurements, not a merge recommendation. Separate harness evidence checkpoint: `278fcd0`.

Optional experimental settings and build tags remain explicit. No PR or merge is part of this checkpoint.
