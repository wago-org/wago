# Experimental compiler checkpoint

Source and tests match qualified commit `72cfa414a9e0d981d670604c23b092cfa3f9ca5b`. Publication sanitizes machine-local paths in experimental evidence and scripts; the complete publication tree therefore differs. Original raw receipts remain retained privately.

Current 145-workload screen: cold compilation latency 22.77% lower than the Go baseline (40% target unmet); arithmetic mean of workload-median absolute peak RSS at the compiled barrier 4.225943 MiB; aggregate execution latency +2.58%, but 51 workloads exceed +5% in the three-trial screen. The per-workload execution gate is NOT met. These are experimental measurements, not a merge recommendation. Separate harness evidence checkpoint: `278fcd0`.

Memory scope clarification: across all 145 workloads and three trials, the compiled barrier is before oracle execution. At the released barrier AFTER oracle execution, the arithmetic mean of workload-median VmHWM is 5.101886 MiB. VmHWM is a process high-water mark, not current live memory. Thus the <5 MiB result holds only through compilation, not the whole observed lifecycle; the whole-lifecycle memory target is NOT met.

Optional experimental settings and build tags remain explicit. No PR or merge is part of this checkpoint.
