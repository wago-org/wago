# Reconstructed promoted-cohort shadow diagnostic

This is newly written source on recovered commit `3548a64d`, not a recovery of
any prior shadow implementation or measurements. Enable `wago_gc_compact_shadow`
(and a compact runtime tag for the native adapter). Never use its timings for
collector comparisons.

Before a minor collection, a separate full exact-root graph trace observes the
previous successful minor's contiguous promotion range. It follows all valid
old and young edges, deduplicates cycles and aliased roots, and ignores null and
i31 immediates. It reports occupied and root-reachable bytes/objects only within
that range. Oversized old allocations between collections are outside the range.
An empty covered range is valid but provides no evidence about object lifetime.

The shadow trace never invokes callbacks or writes heap bytes, roots, starts,
collector marks, or remembered state. Separate scratch is cleared on entry and
exit. It is allocated lazily in diagnostic builds and `ScratchBytes` reports its
backing cost; ordinary `FixedBytes` excludes diagnostic scratch. Default builds
have an empty state and constant-false hook.

Only successful minor collections commit an observation and establish the next
promotion range. Failed minors increment a separate failure count, retain the
previous observation/range, and never contribute partial shadow counts. Every
full attempt invalidates the range; full successes and failures are counted
separately. A malformed graph encountered only by the shadow marks that cycle
invalid; it cannot change the ordinary collection result. Valid totals exclude
invalid and uncovered observations. This distinguishes a true zero-reachable
cohort from missing evidence.

The native adapter returns `CompactShadowDiagnostics` by value, without heap
pointers. Qualification and workload reruns are pending. Neither performance nor
an age2 survivor-policy benefit follows from this source alone.

## New reconstruction qualification

Go1.27.1 serial package qualification passed: default core0.026s, shadow-enabled
core0.015s, shadow race1.177s, and combined cohort+shadow core0.033s. The default
collector remains224 bytes with no enabled diagnostic state and first regular
field at offset0. Initial tagged qualification exposed two fixture assumptions:
the cohort-only footprint assertion did not account for deliberately enabled
shadow state, and a32-byte old heap fit two16-byte test nodes rather than causing
OOM. Tests were corrected to distinguish enabled shadow state and use a16-byte
old heap. No collector/shadow algorithm change was required. The native adapter
wrapper and workload diagnostic rerun are not yet qualified by these core tests.
