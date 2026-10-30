# Stage 2 orchestrator checks

The read-only sol review of `1ad799605c7018457b4b0cef4190c90132e337ac` found no actionable correctness defects. It inspected scalar provenance, full structural replay, option/layout gating, original-PC/root handling, attempt-local ownership, and cycle-local handleless-charge accounting. It noted that mixed-field pressure and caller-frame execution were less directly covered.

The parent added `TestGCLifetimeStage2MixedFieldPressure` and `TestGCLifetimeStage2CallerFrameRoot` in `src/wago/gc_lifetime_pressure_amd64_test.go`. They cover the admitted 32-field boundary with mixed integer/float arguments, early and late field reads, exact NaN payloads and high-bit integer values across helper clobbers/refills; and a live caller object across a non-inlined qualifying callee and actual collections. Both tests assert that the enabled native path was exercised. No production code changed.

Passing commands, with `GOMAXPROCS=8 GOFLAGS=-p=8`:

```sh
go test ./src/wago -run '^TestGCLifetimeStage2(MixedFieldPressure|CallerFrameRoot)$' -count=1 -timeout=10m
go test -tags=wago_regalloccheck ./src/wago -run '^TestGCLifetimeStage2(MixedFieldPressure|CallerFrameRoot)$' -count=1 -timeout=10m
```

Outputs: `pressure.txt`, `pressure-regalloc.txt`. These supplement the full-suite worker's immutable `1ad7996` snapshot. Host reentry and all regional-register combinations are not exhaustively qualified by these two tests. Go test removed temporary build products.

`preflight.txt` records the quiet baseline SHA and missing conformance build tools (`dune`, `ocaml`, `menhir`). The full suite log separately records the actual bootstrap failures and pinned WABT/interpreter state. No system toolchain was installed.
