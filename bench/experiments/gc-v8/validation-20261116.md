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

## Later opt-in cache checkpoint

The parent-array cache remains disabled by default. Its source checkpoint is
local `07ab141` (including the zero-epoch guard); documentation-only commits do
not change execution. Subsequent validation passed:

- Complete unfiltered collector, Wago, AMD64/shared backend, front-end, and spec
  harness packages with `wago_regalloccheck,wago_gc_parent_cache` before the
  final zero-epoch guard; targeted tagged/regalloc cache tests passed after it.
- All 17 official GC files again with the final guard, both explicit and signal
  bounds, opt-in cache and regalloc checks: 91 modules / 574 assertions each,
  zero skips, failures, or admission gaps.
- Complete collectors three times with `wagodebug,wago_guardpage,wago_gc_parent_cache`.
- Complete native and checked collectors under TinyGo 0.42, scheduler tasks.
- Complete unfiltered collector, Wago, and AMD64 backend packages with default
  tags after all source changes (cache disabled).

Independent lifecycle review covered ForcePromote with unchanged backing,
Tiny incremental sweeps, failed promotion rollback, allocation/view refresh,
handle-table replacement, reentry boundaries, saturation, and zero initialization.
Cache entries are not roots. Pointer-bearing cache fields belong to the trusted
native ABI and must not be mutated by hosts. There is no new claim of robust
execution after arbitrary host corruption of native pointers.

Platform and performance limitations in the earlier record still apply. The
miss-heavy regression is why this optimization is explicitly opt-in.
