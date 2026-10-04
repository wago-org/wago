# Experimental Linux/RV64 shared scalar checkpoint

This follows the accepted encoder (`139ef4a8`) and runtime (`e0ff238b`)
checkpoints on the fresh branch from #802's squash
`03a2db03ac06bcf20d5411272a7dce80a2138fae`. The approved plan's bounded
shared-scalar milestone now compiles and executes curated Wasm modules under
baseline QEMU. This is an isolated experimental API, with no public Wago target,
CLI entry, artifact architecture or supported-release claim.

## Admission and ownership

`src/core/compiler/backend/railshot/riscv64.Compile` takes an owned snapshot of
the binary. It uses the normal byte-backed decoder and Wasm validator, then
admits every type, function and export before emitting any instruction.
`BodyBytes` borrow the private immutable snapshot throughout admission and
compilation. The caller must not mutate input concurrently with the snapshot.

The binary section allowlist is custom, type, function, export and code. Plain
function types may contain only i32/i64 parameters and exactly one i32/i64
result. The custom allowlist is `name` and `producers`, treated as inert metadata.
Explicit rec-group/subtype/descriptor syntax, unused unsupported signatures,
unknown custom sections and feature metadata are rejected. The wire pass also
rejects unsupported sections when empty, and explicit singleton rec groups
whose syntax provenance the decoded module does not retain.

All defined functions must pass current `shared.AdmitScalarFunction`, including
unexported functions. Its existing cost heuristic still rejects some tiny
multi-argument leaves. Supported bodies have constants, numeric locals,
nontrapping admitted integer binary operations/comparisons, drop/nop, plain
blocks, structured if/else and tail return. Imports, start, memories, tables,
globals, data/element initialization, tags, string references, FP, SIMD, refs,
loops, arbitrary branches, calls and unsupported integer operations are excluded.
Normal decoding/validation errors remain normal errors; valid excluded shapes
wrap `ErrUnsupported`. No AMD64/ARM64 bytes or interpreter fallback are emitted.

The implementation limits are 1 MiB input, 1,024 functions/types, 256 combined
parameters/locals, the shared admission's 512 operands/32 controls and existing
node/body budgets, 16 MiB logical emitted code and 128 KiB per guest frame.
Decoder metadata reservations are capped at 16 MiB. These are implementation
resource limits, not process-RSS measurements or product limits.

`Compiled` owns immutable heap code/metadata, with no executable mapping.
`Functions` returns copied diagnostics. One shared worker is reused across the
module's functions and `FinishWorker` releases its backing and target references
on exit. Instantiation obtains an Engine, reusable serialized CallFrame and
Program through the previously reviewed runtime publication path. A nonnil
Instance returned with an error is closed and retains failed cleanup ownership;
the caller must Close it again to retry cleanup.

## All 19 hooks and framed convention

The new target implements the merged `shared.ScalarTarget` directly. Shared
code owns values, aliases, registers, spill slots, deferred expressions and
control agreements; none of those allocators were copied or changed.
The physical pool has 20 registers and excludes the encoder's reservation mask,
including SP/RA, GP/TP, FP, Go CTXT/g, provisional context pins and x30/x31 scratch.
`Clobbers` declares the fixed scratch registers; operands are register-only,
`ScaledAdd` is false and `ParameterRegister` is always false.

Entry copies serialized eight-byte parameter cells into valid homes, using
SW for i32 and SD for i64 while preserving the input pointer through the last
parameter. Headers contain saved RA and the result-cell pointer. Local homes
start at byte 16; shared spill/agreement slots follow the entire local range.
The final frame is `align16(16 + 8*locals + 8*sharedSlotCount)` and is checked
against the frame limit before publication. Large offsets and SP adjustments
use reserved scratch rather than overflowing signed-12-bit immediates.

Both bridge preflight and the generated prologue check full frame capacity
before SP changes/frame writes. Reversed bounds also fail before writes.
The prologue's failure edge uses the still-valid trap pointer, sets the stack
trap and returns with original RA/SP. The body may allocate a2/a3; exit never
depends on their original values. Successful exit restores RA/SP and writes
the whole normalized eight-byte serialized result cell.

Every width-sensitive hook handles raw upper bits independently: i32 Move
normalizes even when source equals destination; LWU reloads normalize; SW
stores preserve low-word semantics; BranchZero tests only the low word; both
comparison operands are copied/sign-extended or zero-extended into scratch
before aliases can be overwritten. Arithmetic/variable shifts use *W semantics
then zero-extension, bitwise results normalize, and comparisons return 0/1.
Return normalizes i32 again. BranchCompare emits the false edge requested by
shared compilation. Fixed-size long envelopes retain valid relative offsets
through prologue prepending and module function concatenation.

`Instance.Invoke` accepts noncancelable contexts such as Background/TODO or
WithValue. Nil contexts return an error. Cancellation channels, deadlines or
context errors return `ErrCancelable` before entering guest code. There is no
hidden cancellation bypass. Instance, CallFrame and Engine
locks serialize invocation; Close waits and retries retained failed unmaps.

## Qualification actually run

Environment remains Go 1.27.1 on Linux/AMD64; QEMU user 10.0.13, no-cgo
Linux/RV64 binaries with `GORISCV64=rva20u64`. Guest code stays fixed-width
RV64I/M. QEMU profile:
`rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false`.

| Gate | Executed result |
| --- | --- |
| Full compiler package under baseline QEMU | Passed three repetitions in ordinary and `wago_regalloccheck` builds. |
| Full experimental runtime package under baseline QEMU | Passed three repetitions after serialized-frame integration. |
| Direct raw-hook semantics | 297 edge/seeded input pairs; all 19 admitted binary operations, both widths and three destination alias patterns; all ten comparisons also as false-edge branches. Low-word zero branches, same/different register moves, normalization, small/large stores and loads, and raw Return passed. |
| Actual shared-path modules | Arithmetic/comparison values and fused if branches match independent Go expected semantics on the same inputs; no direct-hook substitute for these module tests. |
| Established AMD64 differential | The same Wasm binaries/inputs for arithmetic, comparisons, branches, pressure and joins match those expectations on the existing public AMD64 backend. |
| Framed pressure | Default pool: 2,320-byte frame, 31 spill slots, 55 spills/31 reloads. Three-register pool: 2,448-byte frame, 47 slots, 88 spills/48 reloads. Both executed expected results. |
| Framed nested joins | 256 locals, live stack prefix and overwritten/tee local aliases; 2,096-byte frame, three agreement slots, 15 reloads; both pools passed. |
| Entry and width boundaries | 256 dirty i32 arguments with large last-home offset; dirty direct returns, full-width result-cell reuse, i64 constant edges; passed. |
| Generated frame fence | Exact capacity succeeds; insufficient/reversed capacity returns stack trap and leaves every below-SP frame sentinel untouched. |
| Admission failures | Validated unsupported imports/state/initializers/features, present-zero DataCount, unused FP type and truly unexported unsupported functions rejected. Invalid modules still fail normal validation. |
| Lifetime and resources | Shared-worker reuse/release accounting, immutable metadata, repeated instantiation, concurrent invocation/Close, serialized-cell capacity/reuse/cleanup retry and active-call retirement passed. |
| Successful steady-state scalar invocation | Zero allocations in `AllocsPerRun`. |
| Portable race tests | New compiler and experimental runtime packages passed on Linux/AMD64. |
| Existing-target compatibility | Core runtime, all encoders, shared compiler and AMD64/ARM64 backends passed ordinary and allocation-check suites on AMD64. |
| Vet/staticcheck | New compiler/runtime passed Linux/AMD64 and Linux/RV64 checks. |
| Documentation/whitespace | Passed. |

Reproduction uses scoped writable caches, `GOWORK=off`, `GOMAXPROCS=4` and
`WAGO_RV64_CLANG=/usr/bin/clang-19`. Build/test commands:

```sh
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 GORISCV64=rva20u64 \
  go test -c ./src/core/compiler/backend/railshot/riscv64 -o /tmp/scalar-rv64.test
qemu-riscv64 -cpu rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false \
  /tmp/scalar-rv64.test -test.v -test.count 3 -test.timeout 60s
# Repeat cross-build with -tags=wago_regalloccheck and run that fresh binary.
go test -race ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
go test -p 4 ./src/core/runtime ./src/core/runtime/riscv64 ./src/core/encoder/... \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/arm64 \
  ./src/core/compiler/backend/railshot/riscv64
# Compatibility check was also run with -tags=wago_regalloccheck.
go vet ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
GOARCH=riscv64 CGO_ENABLED=0 go vet \
  ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
staticcheck ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
GOARCH=riscv64 CGO_ENABLED=0 staticcheck \
  ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
just docs
```

Two independent reviewers inspected the adapter/admission/framing and the
runtime serialization/lifetime boundary separately. Their independent checks
included fresh baseline QEMU builds/tests and portable race/vet checks. A reviewer found
the first hidden-function fixtures actually exported both functions; those
fixtures now contain two functions and only one export, with an explicit shape
assertion. API comments were clarified for noncancelable contexts and retryable
construction-error owners. Final incremental reviews and focused reruns found
no blocking issues. AI assistance was used for implementation and review.

## Remaining scope

This milestone uses the experimental runtime, rather than enabling the public
compiler/runtime bindings. Artifact loading, thunks, prepared/host frames,
signal/cancellation machinery, register-parameter transfer, frame elision and
full-target feature advertisement remain deliberately unwired. The next stages
need genuine RV64 lowering for functions outside shared admission, followed by
the approved integer/control, memory/global, calls, FP/host and SWAR SIMD work.

QEMU results establish correctness evidence only. Josh has no RV64 hardware;
implementation continues without hardware acquisition. Native multi-hart cache
coherence/lifetime stress, native performance and the minimum Go/Linux matrix
remain explicitly unverified. No paid services, new infrastructure, native
performance benchmarks, external publication, push or merge occurred. Full
repository/corpus/official Wasm conformance suites were not run for this isolated
milestone; full before-publication policy still applies. Other checkouts and
concurrent checker work remain untouched.
