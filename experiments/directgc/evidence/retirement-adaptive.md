# Opt-in adaptive retirement comparator

`wago_gc_retire_adaptive` selects map retirement only when the previous completed
retirement had mean object stride <=48 bytes. That conservative threshold covers
the measured positive tiny16/small32/medium48 cases and realistic small-object/root-array mixture; it is not a universally optimal
threshold. First and empty cycles select header traversal. Failed collection
preflight leaves the prediction unchanged. `wago_gc_retire_startmap` remains the
explicit always-map comparator and takes precedence if both tags are set.

The only added state is one boolean in the standalone direct collector. There
is no new native Collector field, hot allocation counter, or default policy
change. Count and retired span already exist at retirement under ABI4.

Alternating small/large cycles can make the one-cycle prediction consistently
wrong. Qualification must include changing-size sequences as well as stable
struct, array and tree workloads. Correctness tests cannot establish a speedup.
Measure matching artifacts with header/adaptive/always-map tags, identical
nursery/old capacities, exact allocation/collection counts, and fresh processes.
Do not promote to default based solely on the isolated traversal microbenchmark.

## Expanded crossover measurement

Additional 100ms x3 microcases on the same host and protocol:

| Shape | Header median | Map median |
| --- | ---: | ---: |
| 48-byte objects | 165.2 us | 90.7 us |
| 64-byte objects | 128.6 us | 94.9 us |
| 80-byte objects | 103.0 us | 91.6 us |
| 4096 small32 + one16400 root, repeated | 219.2 us | 98.8 us |

The selector threshold is 48 bytes: a conservative measured margin rather than
the weakest positive 80-byte case. The root mixture averages about 36 bytes and
would have missed the original 32-byte threshold. This still needs matching
end-to-end validation; mean size is not a complete performance predictor.
