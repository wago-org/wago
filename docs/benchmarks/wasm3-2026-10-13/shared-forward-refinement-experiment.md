> Historical experiment note. The qualified retained combination and current limitations are in [native-shared-parity.md](native-shared-parity.md).

# Shared forwarding experiment policy

This private experiment starts from `0666c2e3`. Its contract is WebAssembly allowed-behavior refinement for an exact adjacent plain i32 store/load pair in one defined, fixed-size shared memory32. It deliberately relaxes the earlier extra promise to preserve each physical guest load: the redundant load may disappear. The store, its bounds check/trap, and the later import CALL remain. No atomic access, callback, synchronization, yield, potentially trapping address operation, profiling observation, or custom instruction may intervene. Unsupported shapes use the original load path.

The semantic argument and primary proposal-draft sources are in the root branch report `shared-memory-forwarding-semantics.md` at commit `01d7ef0d`. This is a restricted experiment, not an atomic-load optimization or a claim of machine-checked correctness. Numeric SDK evidence and source/artifact/fallback/callback/bounds regression checks are required before retention. The independently qualified source0666 captures and local tables must never be relabeled as this experiment.
