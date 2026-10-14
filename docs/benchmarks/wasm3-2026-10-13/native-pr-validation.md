# Native AMD64 PR revalidation

At `2fe87483`, the complete runtime, public API, and AMD64 backend package tests pass in ordinary and register-allocation checker builds on Hub. [The audit](native-pr-validation.json) records the commands, tool versions, log hashes, and complete terminal package results.

The run uses Go 1.27.1, `GOMAXPROCS=1`, CPU 7, and the shared host measurement lock. The official Release 3 fixtures and interpreter are pinned to `9d36019973201a19f9c9ebb0f10828b2fe2374aa`; WABT is pinned to 1.0.41. Both `WAGO_SPEC_INTERPRETER` and `WAGO_SPEC_INTERPRETER_REVISION` must be set, and the pinned WABT directory must precede system tools on PATH.

The first runs exposed missing local fixtures and an older system WABT. With the pinned tools restored, two test fixture helpers still used `--enable-all`, which enabled experimental compact-import wire encodings with an incompatible group count. The existing `spectest.Release3WABTFlags()` list preserves the intended Core 3 encodings; switching the multi-memory and table64 helpers to it makes the existing failing tests pass. A separate Linux example expectation now includes the enabled wide-arithmetic and compact-import features.

These changes correct test inputs and expectations. No decoder acceptance rules or staged conformance baselines were relaxed. This is functional evidence, not a performance or parity claim. Publication remains local.
