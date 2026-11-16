# Dedicated Eden core experiment

Status: correctness-qualified, opt-in research code. **Not a default policy, a
native runtime adapter, or a demonstrated V8 performance win.** The matched
fixed-total-memory experiment did not establish a general throughput improvement.

## Configuration and semantics

Enable `wago_gc_compact_eden`, then call
`NewEdenWithTypes(edenBytes, survivorBytes, oldBytes, types)`. Total young backing
is Eden plus **two** survivor spaces. Existing promote-all and age2 constructors
are unchanged. This policy supports Layout16 only.

New objects allocate in fixed Eden; objects larger than Eden allocate directly
in old space. First survivors occupy the alternate survivor space in deterministic
queue order; overflow promotes. Every live previous survivor promotes. Full
collections move all live objects into alternate old space. Preflight reserves
all destinations before mutating authoritative heap state.

Native `ReadView` and `AllocationWords` return zero for this policy because their
single-young-range ABI cannot represent occupied Eden and survivor intervals.
Cohort/shadow, age2 diagnostics, queue reuse and compact8 combinations reject the
constructor. The core uses pure-Go storage and passes TinyGo core tests; no
TinyGo runtime-adapter support is implied.

## Correctness and performance evidence

Normal, race, age2-coexistence and TinyGo core tests passed, including deterministic
randomized graphs, cycles, aliases, barriers, survivor/remembered overflow,
minimum geometry, allocation-free collection, OOM/invalid-edge rollback and
allocation accounting. Default Wago, backend and GC regression suites also passed.
[Qualification logs](../../bench/experiments/gc-v8/direct-bridge/eden-core-qualification/README.md)
preserve initial fixture/cache failures and successful retries.

[Matched core results](../gc-eden-core/RESULTS.md) retain the original short-run
controls. The longer affinity-controlled confirmation uses the same binary,
128 epochs and five process samples per cell, with our local jobs paused (not an
exclusive-host guarantee). Equal-total Eden is approximately at parity on rolling
retention (0.999 times promote-all's time), but slower on the other controls.
The equal-Eden sensitivity uses 790,528 additional fixed bytes and shows no median
rolling win (1.009 times). Eden incurs six full collections over 128 epochs, so
the initial eight-epoch zero-full observation was transient. These are core-only
results, not end-to-end Wasm or V8 comparisons, and do not justify default enablement.
