# Experimental Linux/RV64 integer/control checkpoint

This extends the accepted shared-scalar checkpoint `e8fc7998` on the
isolated branch from #802's squash `03a2db03ac06bcf20d5411272a7dce80a2138fae`.
It adds genuine whole-function RV64 lowering for validated integer/control
functions outside shared scalar admission. This remains an experimental package,
without a public Wago compiler target, CLI target or artifact architecture.
This document records Stage 4a; the later
[memory checkpoint](riscv64-memory-checkpoint.md) extends its state allowlist
and serialized context layout.

## Selection and bounded admission

Normal binary decoding and validation, owned input bytes, complete-module
section/type/export allowlists and executable ownership are unchanged.
Every function is selected and admitted before any function emits code.
Eligible functions use the merged shared scalar compiler and its 19-hook target;
other functions use the separate integer walker. Tiny leaves rejected by the
shared cost heuristic now use that RV64 path. Truly unexported functions receive
the same checks. No interpreter or AMD64/ARM64 machine code is used.

`CompileWithOptions` can force whole-function fallback for differential tests.
Copied `FunctionInfo.Fallback` records the emitted path; it is not an admission
estimate. Shared worker reuse and release remain unchanged. The separate path
uses fixed temporary registers and canonical operand/local homes, without
copying or changing the shared value/register allocator.

In addition to shared integer operations, the fallback accepts eqz, clz, ctz,
popcnt, signed/unsigned division and remainder, rotates, i32.wrap_i64,
i64.extend_i32_s/u and integer sign extensions. It accepts plain integer/empty
blocks, loops, if/else, br, br_if, br_table, return, unreachable, drop, numeric
select (typed or untyped), constants and integer locals. Indexed block signatures,
block parameters and multiple results remain excluded.

The module allowlist still excludes imports, start, memory, tables, globals,
initializers, calls, FP, references, SIMD, tags and unsupported feature metadata.
Unsupported instructions are rejected even in unreachable code. Validation errors
remain normal validation errors; valid excluded shapes wrap `ErrUnsupported`.
The historical [scalar checkpoint](riscv64-scalar-checkpoint.md) records the earlier,
narrower allowlist.

Fallback admission bounds each body to 64 KiB, 256 parameters/locals, 512 operand
slots, 32 nested controls and a budget of 16,384 local/instruction/table entries.
Each br_table permits at most 1,024 explicit entries; the default is additional.
The existing 1 MiB module, 1,024 functions/types, 16 MiB emitted-code and 128 KiB
frame limits remain. Fixups and branch-site storage are bounded by admission.
These bound compilation resources and frames. Arbitrary guest loops can run
indefinitely; cancellation/deadlines remain rejected before invocation.

## Width, control and framed traps

Serialized i32 arguments lose raw upper bits at entry, and every subsequent
width-sensitive hook retains its low-word contract. RV64 word division,
remainder and rotations are zero-normalized before storage or return.
Division guards distinguish low-word zero from dirty upper bits. Signed MIN/-1
division traps, while signed remainder returns zero. Bit counts use bounded
software loops on baseline RV64I/M; optional extensions remain off.

Control labels carry their canonical base/result slot. Block/function labels
carry a result; loop backedges have zero input arity even for a result-bearing
loop. br_if moves its payload only on the taken edge, preserving fallthrough
operands. br_table uses bounded linear dispatch, keeps its selector intact across
payload copies, and treats its selector as unsigned low-word i32.
Dead controls are still parsed and cannot revive a dead path at their end.
The walker used for admission runs again for emission with the same summary.

Both paths use the reviewed framed prologue/epilogue. Capacity/reversed-bound
checks precede SP changes and writes. Fallback additionally zeros declared local
homes on every entry. Its fixed temporaries preserve the original A2 trap-cell
pointer. Unreachable/division guards jump to deferred trap islands and then the
common epilogue, restoring original RA/SP before returning to the bridge.
The isolated bridge uses experimental codes: 1 stack fence, 2 unreachable,
3 divide by zero, 4 signed overflow. These are not the production runtime's
TrapCode namespace; production integration remains a later boundary.

Executable publication, process-wide Linux icache synchronization, W^X,
serialized call frames, invocation/retirement locking and cleanup retry use the
previously reviewed runtime unchanged.

## Qualification actually run

Host: Go 1.27.1 Linux/AMD64. Execution: workspace-local QEMU user 10.0.13;
fresh no-cgo Linux/RV64 test binaries, GORISCV64=rva20u64. Guest instructions stay
fixed-width baseline RV64I/M. QEMU profile:
`rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false`.

| Gate | Result |
| --- | --- |
| Full compiler under baseline QEMU | Passed three repetitions, ordinary and wago_regalloccheck builds. |
| New arithmetic | Both widths, div/rem/rotate and 16 unary/conversion opcodes on 297 edge/seeded input pairs; independent Go/bits expectations passed. |
| Trap reuse | Divide-by-zero/overflow and unreachable followed by success on reused stack/trap/result cells passed. |
| Genuine shared versus forced fallback | All 19 shared binary operations, both widths, plus ten comparison/if branches; both paths matched independent expectations. Actual path metadata asserted. |
| Structured control | Function-label/early returns, conditional payload preservation, result-loop backedges, deep loop exits, dead nesting, if trap/no else, repeated/last/default br_table targets and dirty selectors passed. |
| Select/local reuse | Both integer widths, typed/untyped selects, dirty i32 values and zeroed declared homes across calls passed. |
| Large frame | 256 locals, 301 operand slots, 4,480-byte frame with branch payload/reloads beyond signed-12-bit offsets executed the expected result. Successful steady-state fallback invocation measured zero allocations. |
| Established AMD64 comparison | Same control and deep-frame Wasm fixtures matched the same expectations, including trap versus success and repeated reuse. Prior scalar fixture differential retained. |
| Admission | Hidden unsupported functions, dead FP, indexed signatures and instruction/control/operand/table bounds rejected before publication. Existing module/feature denials retained. |
| Independent review | Two readonly reviewers found no production or frame/ABI blockers; invalid early-return test fixture corrected. Suggested table/select/deep-frame regressions added. Both independently reran fresh QEMU subsets successfully. |
| Existing-target compatibility | Nine focused runtime/encoder/shared/AMD64/ARM64/new RV64 packages passed ordinary and allocation-check suites on AMD64. |
| Portable race | New compiler/runtime passed. |
| Vet/staticcheck | New compiler/encoder/runtime passed on Linux/AMD64 and Linux/RV64. |
| RISE workflow | Bash syntax, shellcheck, actionlint and independent review passed locally. AMD64 execution correctly rejects. No physical execution performed. |

Focused reproduction:

```sh
export GOWORK=off GOMAXPROCS=4 GOCACHE=/tmp/wago-rv64-go-cache
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 GORISCV64=rva20u64 \
  go test -c ./src/core/compiler/backend/railshot/riscv64 -o /tmp/rv64-integer.test
qemu-riscv64 -cpu rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false \
  /tmp/rv64-integer.test -test.count 3 -test.timeout 3m
# Repeat build with -tags=wago_regalloccheck before executing that binary.
WAGO_RV64_CLANG=/usr/bin/clang-19 go test ./src/core/encoder/riscv64
```

## RISE native CI, local only

The new [.github/workflows/riscv64.yml](../../.github/workflows/riscv64.yml)
uses exactly `ubuntu-24.04-riscv`, following the official
[RISE label reference](https://riscv-runners.riseproject.dev/docs/getting-started/labels.html).
It leaves established CI jobs and required status planning unchanged.
Same-repository PR/manual triggers use contents:read, nonpersistent checkout
credentials, per-ref cancellation, one 20-minute job and two Go workers.
Native Go 1.27.1 is extracted into job-local storage after official release
manifest/checksum verification; no persistent installation or sudo is required.

[scripts/ci-riscv64.sh](../../scripts/ci-riscv64.sh) first checks native Linux/RV64.
Its smoke stage exercises actual executable allocation/publication, process-wide
flush, ABI sentinels, stack fences, cross-thread/GC publication and framed
control/deep-frame calls. Only after smoke succeeds does it run the supported
compiler/encoder/runtime suites, including the compiler allocation-check build.
No benchmark or broad specification-suite qualification is claimed.

Connected GitHub metadata verified jtenner repository admin/push access.
The readonly org installation endpoint lacked admin:org scope, so RISE App
installation is unknown; no OAuth expansion, App install or credential change
was attempted. An organization owner must verify/install the
[organization App](https://github.com/apps/rise-risc-v-runners) for wago-org,
select wago, and ensure the dedicated RISE runner group permits this repository.
The [official setup](https://riscv-runners.riseproject.dev/docs/getting-started/install.html)
requires Self-hosted runners read/write and Metadata read.

The user explicitly held publication. No push, PR or remote CI occurred.
Physical-runner execution is pending both confirmed access and later publication
approval. QEMU evidence establishes correctness only. Native cache/lifetime
stress, performance and release qualification remain unverified.
