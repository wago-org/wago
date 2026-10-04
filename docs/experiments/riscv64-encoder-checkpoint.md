# RV64 encoder validation checkpoint

Baseline: `03a2db03ac06bcf20d5411272a7dce80a2138fae` (#802 squash).
Branch: `feat/rv64-encoder-checkpoint`. See the
[port contract](riscv64-port-contract.md) for scope and subsequent gates.

This records the encoder-only checkpoint at `139ef4a8`. The subsequent
[runtime checkpoint](riscv64-runtime-checkpoint.md) adds QEMU execution evidence;
the execution limits below describe the earlier checkpoint only.

The change adds only `src/core/encoder/riscv64` and these experiment documents.
It is not imported by a production backend. Existing targets, public features,
runtime assembly and native artifact IDs are unchanged. No #284 code was used.

## Evidence

Executed on Linux/AMD64 with Go 1.27.1 and Debian clang 19.1.7. Package tests
run host-side byte/decoder checks; they do not execute generated RV64 code.
Commands below were run from the repository root with `GOWORK=off`,
`GOMAXPROCS=4`, and a writable scoped `GOCACHE`.

| Check | Result |
| --- | --- |
| New encoder tests, ordinary and `wago_regalloccheck` | Passed. |
| Live LLVM assembler comparison | All 46 fixed RV64G instruction words matched; ELF has no unresolved relocations. |
| Constant properties | Edge/power-of-two cases and 8,192 seeded i64 values in four physical destinations; 4,096 seeded i32 values; passed. |
| Width/alias cases | Raw word sign extension, explicit zero normalization, arbitrary incoming upper bits and destination aliases; passed. |
| Fixup properties | Exact asymmetric reach, signed low-12 carry, forward/backward/repeated fixups, worker concatenation, malformed envelope fields and transactional errors; passed. |
| Short control flow | Every aligned B-type displacement plus JAL endpoints/interior bit positions and 4,096 seeded displacements in three link registers; passed. |
| Successful preallocated constant + fixup sequence | Zero allocations in `AllocsPerRun`. |
| Five-second constant fuzz, two workers | 329,107 executions; passed. |
| Five-second long-fixup fuzz, two workers | 559,717 executions; passed. |
| New package statement coverage | 98.8%. |
| AMD64/ARM64 encoders, shared scalar compiler, AMD64/ARM64 backend packages | All six packages passed in ordinary and allocation-check builds on this AMD64 host. |
| `go vet` and `staticcheck` for the new package | Passed. |
| Documentation checker and whitespace/format checks | Passed. |
| No-cgo Linux/RV64 encoder test cross-build | Passed; binary not executed. |
| No-cgo Linux/ARM64 backend test cross-build | Passed; binary not executed. |

Representative reproduction commands:

```sh
WAGO_RV64_CLANG=/usr/bin/clang-19 go test -count=1 -cover ./src/core/encoder/riscv64
WAGO_RV64_CLANG=/usr/bin/clang-19 go test -tags=wago_regalloccheck -count=1 ./src/core/encoder/riscv64
go test -p 4 -count=1 ./src/core/encoder/... \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/arm64
go test -p 4 -tags=wago_regalloccheck -count=1 ./src/core/encoder/... \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/arm64
go test ./src/core/encoder/riscv64 -run '^$' -fuzz '^FuzzConstant64$' -fuzztime=5s -parallel=2
go test ./src/core/encoder/riscv64 -run '^$' -fuzz '^FuzzLongFixup$' -fuzztime=5s -parallel=2
go vet ./src/core/encoder/riscv64
staticcheck ./src/core/encoder/riscv64
just docs
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 go test -c ./src/core/encoder/riscv64 -o /tmp/rv64-encoder.test
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./src/core/compiler/backend/railshot/arm64 -o /tmp/arm64-backend.test
```

`TestAssembler` requires an explicitly configured RISC-V-capable clang; it skips
when `WAGO_RV64_CLANG` is unset. The LLVM 19 gate above was executed, not skipped.
The clang on this host's default PATH is an Emscripten build lacking the required
RISC-V backend; the explicit Debian binary avoids that unrelated tool limitation.
Staticcheck's default cache was outside the writable workspace; rerunning with
a scoped `XDG_CACHE_HOME` succeeded.

## Independent review

An independent reviewer read all new source/tests against the port contract,
checked physical Go register reservations, constant rounding, i32 normalization,
long-envelope inversion, relocation and atomic patch failure. No blocking findings
remain. The reviewer independently ran the LLVM comparison, package tests and
vet, plus five-second constant/fixup fuzz runs (324,675/353,204 executions).

Two suggested test improvements were incorporated: JAL interior displacement
properties and rejection of corrupted AUIPC destination/JALR base, funct3 and
link fields. The reviewer inspected those additions and confirmed the no-blocker
result. There were no production-code fixes requested. AI assistance was used
for implementation, tests, contract documentation and independent review.

## Limits and next gate

Full repository, corpus and official Wasm conformance suites were not run for
this unconnected encoder checkpoint. The before-publication full-suite policy
still applies. Neither RV64 QEMU/native execution nor native ARM64 execution
was run. There are no performance measurements or native qualification claims.

The next reviewable stage is the Linux foreign-stack/runtime bridge: framed
stack bounds and ABI sentinels, W^X publication and process-wide instruction-cache
synchronization, cleanup and lifetime. Then the 19-hook adapter and strict
whole-module scalar admission can follow. This checkpoint adds no fallback
compiler and admits no unsupported Wasm module shapes.
