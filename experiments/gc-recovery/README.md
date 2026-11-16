# Recovered checkpoint verification

The cloud workspace was replaced during publication. The repository was restored from verified remote `cc33d1037757988bf5e4a79eab4c59d38a26c1f4`. Nine pending cohort commits were reconstructed solely from already uploaded Git blobs; every intermediate tree matched the saved original manifest. The recovered remote checkpoint is `3548a64d1e2afd3aa3bcc9851a047bffa1b5c0e4`, tree `db0dda5cf1ad8c53061e55b1e3a2c2b8d5c3f89b`.

Fresh verification used official Go 1.27.1, TinyGo 0.42.0, and pinned spec-v3 corpus `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Unfiltered default Wago, native/checked GC, AMD64 backend, direct and compact cores passed; native debug passed. TinyGo initially failed its known duplicate task-exit linker symbol. Applying the repository's existing `scripts/patch-tinygo-042.sh` compatibility patch restored passing native and wrapper tests. Logs are retained here, including the failed setup attempt.

Later SSE, shadow and weighted work is not implied recovered or qualified by this checkpoint. Reconstructed code needs explicit source-identity evidence and fresh tests. Historical timings remain historical; this verification does not establish the V8 target.

## Integrated core/shadow/SSE qualification (2026-11-16)

At source `8bbb5885`, the merged age-two core, reconstructed shadow diagnostics,
and hash-verified restored SSE source passed fresh serialized tests on Go 1.27.1:

- Default Wago 49.986s, native collector 2.885s, wrapper 0.005s,
  amd64 backend 10.322s, direct core 0.027s, compact core 0.018s.
- `wagodebug` native collector: 2.667s.
- Applicable compact SSE + refstruct + regalloc runtime matrix: 0.996s.
- Patched official TinyGo 0.42.0, tasks scheduler: native 3.155s, wrapper 0.004s.
- Compact core default, age2, shadow, cohort+shadow, age2+cohort and
  age2+shadow combinations all passed; age2 race passed 1.139s.

Independent review identified two footprint-test assumptions that did not compose
with other enabled experimental states. Commit `57ca4f49` checks each disabled
state independently and reserves the zero leading-offset assertion for the fully
disabled build. No production collector change was required for those failures.
Age2 constructors explicitly reject promote-all diagnostic combinations rather
than emit misleading accounting. Age2 remains core-only in this checkpoint;
its native runtime adapter and weighted reduction are not included.

The recovered host is Intel Xeon Platinum 8573C, not the historical AMD EPYC 9V74.
No performance claim follows from these correctness tests; future comparisons
must rebuild both variants on the same new host. Historical SSE raw timing files
and binaries have not been restored by source reconstruction.
