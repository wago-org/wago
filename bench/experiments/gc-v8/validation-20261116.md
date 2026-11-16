# Integrated checkpoint validation

Code tree `8ba3370e3887f3f0b95dfdc541ed51ec11a225c0`, published as
`5d43f5bb95d9adfc3ecfbf3f83e50c33be2eb6d0` (equivalent local `bd7fe52`).
Linux amd64, Go 1.27.1; TinyGo 0.42 with Go 1.27.1.

Passed, no test-name filtering:

```sh
go test -count=1 -tags wago_regalloccheck \
  ./src/core/runtime/gc/... ./src/wago \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/frontend ./tests/conformance/spectest
go test -count=3 -tags wagodebug,wago_guardpage ./src/core/runtime/gc/...
tinygo test -scheduler=tasks ./src/core/runtime/gc/native ./src/core/runtime/gc
```

Official spec-v3 pinned at `9d36019973201a19f9c9ebb0f10828b2fe2374aa`:
all 17 `test/core/gc/*.wast` files executed with the pinned official interpreter
and WABT 1.0.41. Both explicit bounds (`wago_regalloccheck`) and signal bounds
(`WAGO_BOUNDS=signals`, `wago_regalloccheck,wago_guardpage`) passed 91 modules and
574 assertions, with zero failed, skipped, or admission gaps. The command was
`go test -count=1 -run '^TestSpecSuiteExec$' -v ./src/wago` with the corresponding
tags and `WAGO_SPEC_FILES` listing all 17 GC files, not the entire Core v3 corpus.

The unfiltered Wago package includes the collection/digest audit and new loop
trap/lifetime regressions. Independent source review found no blocking defect in
sparse mark reset, resolved tracing, profile metadata, loop-address lifetime,
or survivor-occupancy feedback.

This is not cross-platform qualification: no arm64 execution, complete Core v3
corpus execution, or representative application tail-pause study was performed.
The V8 performance target remains a separate, unmet gate.
