# Isolated baseline qualification

Source: `9713878ab452848f7eb7549a90be94b938a898be`. The worker verified all 7,374 tracked blobs before and after testing. `report.txt` records exact commands, environment, failures and cleanup. `reproduce.sh` uses a disposable archive and pinned fixtures, preserving the caller's checkout.

Both ordinary and regalloccheck root suites passed 136 packages and failed only `src/wago` staged conformance tests. The original shared-checkout baseline attempt was contaminated by concurrent source edits and is superseded by these isolated results.

## GAP-CONFORMANCE-TOOLCHAIN

Full staged conformance is unqualified. The installed WABT `1.0.42` differs from pinned `1.0.41`; after bootstrapping and verifying `1.0.41`, 18 staged tests still required the pinned official WebAssembly Release 3 interpreter via `WAGO_SPEC_INTERPRETER`. Its bootstrap failed with **`dune is required to build the official Release 3 interpreter`**. See `interpreter-bootstrap.txt`, the two `staged-wabt-1.0.41*.txt` logs and `wabt-bootstrap.txt`.

Closing this gap requires providing `dune` and the interpreter build prerequisites, successfully running `scripts/bootstrap-spec-interpreter.sh --verify`, setting `WAGO_SPEC_INTERPRETER` to that pinned interpreter, and rerunning staged conformance with WABT `1.0.41` on both baseline and port. No conformance baselines were rewritten, no engine defect is established by these missing-tool failures, and no full-green suite is claimed.

Temporary source exports, fixtures, tools and build artifacts were removed by the worker. Retained files here are evidence and the reproduction script.
