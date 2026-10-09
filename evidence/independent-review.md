**Keep the local #815 diagnostic prototype; extend it before claiming full issue acceptance.** No substantive code blocker remains in implementation commit `236b6b7a2ba8af313487d610e26bc690272ee812`.

Independent verification passed with `GOMAXPROCS=2`, `-p=2`, and private `/tmp` cache:

- Ordinary package tests: passed, 0.005s.
- Profiled XOR/Fibonacci fixture tests: passed, 0.021s.
- `git diff --check` against verified base `5cd443965a09e46076cce5e3dd4bf3ae69500348`: passed.

Controls distinguish constants, dependencies, widths and displacements while preserving raw bytes and accepting explicitly matched JMP/B relocations. XOR attribution reproduced with two supported instructions and six public-API oracle executions. Existing Fibonacci reproduced 12 mapped instructions: six supported, six unknown, correctly incomplete.

Results match the raw timing, allocation, binary-size and hash evidence. Ordinary runtime binaries are byte-identical; noisy timings establish no speedup or regression.

Limits remain partial instruction coverage, producer-asserted identities, selected-region qualification, unmeasured retained memory/RSS and no native ARM64 execution. Correct the report’s “omitted regions” wording to distinguish one-sided missing regions from image bytes omitted on both sides. No source or evidence files were edited during this review.
