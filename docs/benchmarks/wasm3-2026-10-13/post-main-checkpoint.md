# Post-main integration checkpoint

PR #963 retains main integration and the mixed-memory cache fix through `6eb3a1df`. Broad Apple ARM64 ordinary/checker checks and focused profiling checks pass. The mixed-memory regression also passes native Linux AMD64. Remote full AMD64 validation was interrupted externally; no complete result is claimed.

Two additional committed private candidates remain outside the PR and local publication:

- `27e0904f` restores shifted-i32 dot vectorization under literal-register pressure, fixing the post-merge audio-autocorrelation slowdown. Its full 532-capture comparison and 373-contract compatibility run complete without outcome changes.
- `df0b21cd`, based on that fix, preserves validated native helper pins in fixed-shared import loops. Nine-launch reversed rechecks improve ordinary thread helpers by 6.8%/7.0% and opaque helpers by 10.0%/7.0% versus `27e0904f`. Targeted loops remain approximately 1.5 times slower than pinned w2c2 Clang. Compact import remains approximately 1.03–1.04 times peer cost.

For `df0b21cd`, all 532 full performance captures complete without functional changes or resource failures against the last published `9ed26450`. Across 255 numeric pairs, steady geometric mean improves 5.9%; compile time increases 8.6% and compile RSS increases 3.1%. Feature steady geometric mean increases 0.36%. All 373 compatibility contracts complete: 346 pass, 27 unsupported, zero fail.

A separate five-launch, reversed-order public Compile control over 145 core applications compares `df0b21cd` with current main `069a26ea`: compile time increases 8.1%/8.6%, allocations increase 0.42%, and process peak RSS increases 2.9%/2.9%. This standalone control does not replace SDK resource measurements or establish an execution delta against main.

The 255-image audit against `27e0904f` changes exactly the two targeted thread helper images (944 to 960 bytes). The environment rollback reproduces all 255 original images. Effectful Go callbacks retain their fallback and verified callback counts.

Full-corpus reversed outlier qualification is still running. These candidates are not retained in the PR or published while that review is pending. The empty-entry normalization candidate `321857f4` is held because both focused orders regressed.

The local server remains healthy at http://localhost:8788 and serves qualified source `9ed26450`; browser verification and publication receipts are recorded in `local-publication-pure-callee.json`. No production deployment. Detailed captures and durable job state remain under `.validation/wasm3-parity`; the active controller is `native-leaf-amd64/finish-pure-shared-pins.py` and does not automatically retain or publish candidates.
