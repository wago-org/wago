# Direct16 paired/range attribution

Source-only protocol, awaiting qualified source and CPU release.

Three same-source binaries: baseline.test (direct trusted loop + scalar weighted reduction), paired.test (+ direct_pair_loop), range.test (+ direct_range_loop). Identical checked allocation/fill/copy configuration and 2 MiB young / 2 × 8 MiB old budget. Age2 and elision OFF.

Audit all six exact artifacts at 128 warmups requiring actual collection, checksums, allocations and direct receipt. Then 3 rotating rounds of 3 warmup + 5 calls. Full gate uses 100 separate warmups, 9×32 calls, 3 rotating rounds against fresh Node --no-liftoff. Keep all controls and misses; <=1.15× applies per case. These are compiler-stage differences within direct16, not isolated GC or layout effects. Existing V8 internal allocation-elision uncertainty remains.

Record source/binary/artifact hashes, versions and CPU; do not reuse historical different-host timing. Follow the existing direct probe compilation-footprint protocol separately.
