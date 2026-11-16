# Opt-in adaptive retirement comparator

`wago_gc_retire_adaptive` selects map retirement only when the previous completed
retirement had mean object stride <=32 bytes. That conservative threshold covers
the measured positive tiny16/small32 cases; it is not a universally optimal
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
