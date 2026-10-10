# PR #895 independent cleanup evidence

Retain [PR #895](https://github.com/wago-org/wago/pull/895). Its constant
division/remainder pairing, predicate shifts, contiguous SIMD alignment,
bounded two-lane integer reductions, and owned numeric import binding remain
distinct from #909's general memory-loop replication and #910's sum-unroll
factor experiment. Neither related branch supersedes these changes. Their
worktrees and the local RISC-V implementation were left untouched.

## Revisions and review

- Original author: `jtenner`; head `53d6c164875f9ef18ade5c5d266816d273a0c7a0`,
  original base `2286d676facdfa1cabe2d1c61072e505438ca7f2`.
- Integration commit `11802f9867e7b46a3a21b68444ac377b6616782b` merges current
  main `65194f23fc12222e006ccbf79d9880ca38182b23` (#908). The changelog keeps
  both entries; its numeric invocation admission change is preserved.
- Minimal fix: `f7a3506b0abf79d702f71d7a18746ec55ced5fca`.
- Existing Codex code/security reviews reported no findings. There were no
  inline review threads or submitted reviews. The human request was to inspect
  more closely and get CI passing. A separate worker reviewed the original
  full diff, then the integration and unlock fix, reporting no findings in
  both passes. Its review was source inspection; execution evidence is below.

## TinyGo regression and fix

The existing `TestCallerResolverInvocationContextReentryLifetimes` passes on
the original base with TinyGo's default task stack. On the integrated PR it
prints a passing test, then fails the task stack canary. Disabling the new
integer reduction does not prevent the failure; increasing the stack to
128 KiB masks it. Neither workaround is part of this fix.

The `defer fn.mu.Unlock()` added in the large `bindSyncHostImport` switch makes
TinyGo reserve return temporaries across the switch. Recursive gated callback
binding compounds that cost. A local hardware watchpoint attributes the first
canary write to this binding path during instantiation. Function prologues
reserve 6,360 bytes on the original base, 23,560 bytes before cleanup, and
6,424 bytes after cleanup (plus the same 48 bytes of register pushes).

The fix unlocks explicitly on each of the four locked return paths, copying
the owner binding or callback while locked. It preserves synchronization and
adds no allocation. The existing regression passes afterward, as does the
full five-package TinyGo suite on the default task stack. No duplicate test
or CI stack-size change was added.

```sh
tinygo test -tags=wago_regalloccheck -v -scheduler=tasks \
  ./internal/regalloccheck ./src/core/encoder/amd64 \
  ./src/core/encoder/arm64 ./src/core/runtime ./src/wago
```

## Short performance and memory check

[Raw paired samples](runtime-performance-cleanup-20261009.json) compare the
integrated pre-fix commit with the fix on native Linux/amd64, Ryzen 7 8845HS,
Go 1.27.1. Precompiled binaries alternate order for six pairs, pinned to CPU 2
with `GOMAXPROCS=1`; there is no concurrent compilation. Existing benchmarks
use 200 ms per sample. Medians are descriptive, not a statistical claim of a
runtime speed change; the short samples have visible timing variation.

| Benchmark | Before ns/op | After ns/op | Before/after B/op | Before/after allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InvokeHostFuncDirect | 128.75 | 133.75 | 64 / 64 | 1 / 1 |
| RuntimeInstantiateOwnedHostFuncref | 15113.5 | 14492 | 3328 / 3328 | 41 / 41 |
| HostFuncRefNumericImport | 137.25 | 133.35 | 0 / 0 | 0 / 0 |

An evidence-only owner create/close probe uses exactly 10,000 iterations per
sample because its reference registry grows until `Runtime.Close`. Both
versions use 449 B/op and six allocations/op. The probe was removed after
building measurement binaries. The fix changes no retained data structure.

The original PR adds one pointer to each `HostFuncRef` (128 to 136 bytes on
amd64). Numeric dynamic imports retain both direct and reference thunks:
the lifecycle test reports 60 bytes of shared reference-thunk code in a
4,096-byte mapping per `Compiled`, plus a 4,096-byte owned mapping per instance.
Existing close/rejected-close/foreign-runtime tests verify callback lifetime
and clearing. Static imports have a separate test for omitting the shared
thunk. The reduction planner remains bounded to 64 expression nodes, 12
locals and 128 body operations; generated reductions use at most 14 spill
slots. The original benchmark JSON is historical base-versus-PR evidence,
not a fresh claim against main after #908.

## Validation and limits

Local native Linux/amd64 validation uses Go 1.27.1, TinyGo 0.42.0/LLVM 22.1.4,
and pinned WABT 1.0.41:

- Full root ordinary and `wago_regalloccheck` Go suites; full checked bench
  module suite; focused checked host lifecycle and race suites.
- Guard-page/codegen-stat tests for division pairs, predicate shifts,
  reductions, pure selects and CMOV; profile-enabled SIMD register-pressure
  tests for all feature/compact configurations.
- Full five-package TinyGo task-scheduler suite, focused regression, targeted
  `go vet`, documentation checker, and whitespace check.

Local root suites require native socket/filesystem permissions and
`GOFLAGS=-buildvcs=false` for temporary standalone TinyGo module builds.
Initial sandbox permission failures, the local temporary-module VCS stamping
failure, and an initially wrong WABT 1.0.42 are environment failures, not
passing runs. Corrected runs above are the validation evidence.

Original-head full CI run 37889586030 failed TinyGo's stack canary and a
Windows/arm64 nested Bash completion process. The Windows job passed its
retry without source changes; TinyGo remained failed until this fix.
Published-head CI must be checked separately. Local results cover native
Linux/amd64 only; remote matrix runners provide native six-platform Go
qualification. The arm64 encoder unit package running on amd64 is not native
arm64 runtime qualification. Full qualification must not be inferred from a
draft smoke run or emulated tooling.

Human review remains required. This cleanup authorizes no merge or closing
of this or related PRs.
