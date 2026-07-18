# Linux/RISC-V 64 backend port

This document is the implementation contract for wago's `linux/riscv64` native
JIT target. The port follows the ARM64 backend's fixed-width writer, foreign-stack
runtime, and architecture-subpackage structure while keeping RISC-V-specific
branch, instruction-cache, alignment, and vector constraints explicit.

## Current status

- RV64G scalar instruction writer: complete and golden-tested.
- Linux no-cgo foreign-stack runtime: integrated and green under
  `qemu-riscv64`, including synchronous host re-entry.
- Production railshot backend: integrated for scalar integer and floating-point
  operations plus complete core SIMD/relaxed-SIMD through an RV64G SWAR tier,
  structured control, direct/indirect calls, explicit-bounds memory, bulk memory,
  globals, tables, references, traps, and wrapper/internal ABIs.
- Public end-to-end corpus execution matches the amd64 backend for the scalar
  manifest workloads, including recursive, FP-heavy, crypto, Rust, and
  AssemblyScript modules.
- The curated WebAssembly 1.0 execution suite passes under QEMU with 629 modules
  and 16,026 assertions, with no failures or feature gaps. The Linux/RV64 public
  API suite also runs the Release 2 bulk-memory, multi-value, table, funcref,
  externref, imported-memory, and snapshot tests enabled for this target.
- `GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 go build ./...` succeeds.
- Linux guard-page execution is integrated under `-tags wago_guardpage`, including
  lazy page commitment after `memory.grow`, out-of-bounds trap rewriting, imported
  guarded memory, reservation reuse, and full public-suite execution under QEMU.
- Cooperative context cancellation polls at function entries and loop headers;
  native and synchronous host-call loops are interrupted under QEMU and the trap
  cell remains reusable after cancellation.
- All 256 decoded SIMD and relaxed-SIMD instructions lower through baseline
  RV64G SWAR with two-GPR `v128` values. Public globals, locals, memory, control,
  direct/indirect/cross-instance calls, and synchronous host calls use adjacent
  little-endian uint64 ABI slots. The official current SIMD suite passes 473
  modules and 24,335 assertions under QEMU; its one multi-memory module is the
  separately reported project-wide feature gap. Relaxed SIMD passes 8 modules
  and 69 assertions with zero gaps.
- RVV 1.0 host capability detection is centralized in the runtime using Linux
  `riscv_hwprobe` plus process `AT_HWCAP`; the RVV encoder foundation executes
  under QEMU and safely skips on baseline RV64G machines. It remains reserved for
  an optional future optimization tier.
- Conservative SWAR register-lifetime optimizations are enabled: one loop-hot
  call-free v128 local may occupy an atomic GPR pair, reducing the checked fixture
  from 25 to 15 static load/store words at unchanged 248-byte code size; caching
  one v128 constant repeated at least three times reduces its fixture from 304 to
  200 code bytes.

## Target baseline

The execution baseline is RV64G as exposed by Go's `linux/riscv64` target:
RV64I plus M, A, F, D, Zicsr, and Zifencei. Generated code does not require the
compressed or vector extensions. WebAssembly SIMD is always available on this
backend through complete SWAR lowering, independent of RVV.

Optional RVV-tier capability detection requires both the ratified V 1.0 bit from
`riscv_hwprobe` and the V bit in process `AT_HWCAP`. The former avoids mistaking
vendor RVV 0.7.1 HWCAPs for the incompatible ratified encoding; the latter
respects Linux's process vector-state policy. The single-letter V 1.0 extension
depends on `Zvl128b` and `Zve64d`, so a successful probe establishes VLEN >= 128
and vector f32/f64 support. Missing, unknown, or contradictory sources disable
only the future RVV optimization tier, not WebAssembly SIMD semantics.

The hardware probe uses a nil CPU set, asking Linux for the logical intersection
across all online CPUs. This keeps generated RVV safe when Go moves execution
between OS threads and harts. Do not replace it with `/proc/cpuinfo` parsing or
HWCAP-only admission. HWCAP by itself is ambiguous on older vendor kernels, and
`riscv_hwprobe` by itself does not express the process's vector-state permission.

Linux is the first and only RISC-V operating-system target in this port. Darwin,
Windows, riscv32, big-endian RISC-V, and bare-metal execution are out of scope.

## Verification commands

From an amd64 development host with `qemu-riscv64`:

```bash
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 go test -c \
  -o .validation/riscv64.test ./path/to/package
qemu-riscv64 .validation/riscv64.test -test.v
```

QEMU is a correctness and CI gate, not a performance oracle. Performance and
instruction-cache claims require native RISC-V hardware measurements.

## Runtime boundary

Generated code runs on a dedicated off-heap foreign stack. It must never run on a
movable goroutine stack. The trampoline preserves the Go context, including:

- `X26` (`CTXT` / `S10`);
- `X27` (`g` / `S11`);
- `X3` (`GP`) and `X4` (`TP`);
- `SP`, `RA`, and the remaining psABI callee-saved integer registers.

Executable publication is W^X: map RW, copy, change to RX, then invoke Linux
`riscv_flush_icache` (system call 259) for the emitted address range. `FENCE.I`
alone is not the process-wide publication primitive because the executing thread
may migrate between harts.

The checked-in `src/core/runtime/riscv64spike` package is the go/no-go proof for
this boundary. It executes integer, branch, memory, and scalar-FP code under
QEMU, verifies the foreign SP, restores callee-saved state, and survives repeated
GC cycles.

## Native ABI register roles

The production backend uses these fixed roles:

| Role | Register | Notes |
|---|---|---|
| integer args/results | `A0..A7` | psABI; primary result in `A0`, second integer result in `A1` |
| float args/results | `FA0..FA7` | psABI |
| wrapper args | `A0=serArgs`, `A1=linMem`, `A2=trap`, `A3=results` | same logical order as amd64/arm64 |
| pinned linear-memory base | `S9/X25` | callee-saved; avoids Go's `CTXT` and `g` |
| explicit mem-size cache | `S8/X24` | present only for explicit-bounds memory functions |
| hot integer locals | `S3..S7`, then selected temporaries in call-free code | call boundaries spill/reload pinned locals explicitly |
| fixed address scratch | `T5/X30` | never allocated |
| far-transfer scratch | `T6/X31` | never allocated; also Go assembler `TMP` in assembly sources |
| merge register | `S2/X18` | reserved from local pinning when register merge is active |
| Go runtime reserved | `S10/X26`, `S11/X27` | `CTXT` and `g`; generated code never allocates them |
| unavailable | `Zero, RA, SP, GP, TP` | architectural, call, process, or thread roles |

Module-wide global value pinning is disabled in the initial production baseline.
Globals remain canonical in their runtime cells across every call boundary. This
is deliberately conservative; it avoids hidden cross-function register
invariants until native RISC-V pressure and performance measurements justify the
optimization.

The backend must save or spill every caller-visible value before calls according
to the psABI. It must not allocate `X26` or `X27`, even in call-free functions:
async signals and Go runtime inspection require `g` to remain valid while native
code is running.

## Control-transfer policy

Base conditional branches reach only approximately plus or minus 4 KiB. The
backend must not emit a short placeholder and hope layout remains in range.
General conditional transfers use the encoder's fixed 12-byte form:

```text
b.<inverse> +12
auipc scratch, hi20
jalr x0, scratch, lo12
```

Direct calls and unconditional transfers use patchable `AUIPC+JALR` pairs unless
a deliberately measured local fast path proves `JAL` range. This keeps the
single-pass compiler deterministic and avoids moving emitted code during
relaxation.

## Linear memory

The initial production backend uses explicit bounds checks. Wasm permits
unaligned accesses. Linux's RISC-V userspace ABI guarantees scalar misaligned
access support, whether directly in hardware or through transparent kernel
handling, so the Linux/RV64 backend may issue direct scalar loads and stores for
Wasm accesses. The qualification suite exercises every byte offset modulo 16 for
split v128 loads/stores. `RISCV64MisalignedScalarPerformance` records Linux's
all-online-CPU hwprobe classification (`unknown`, `emulated`, `slow`, `fast`, or
`unsupported`) for optimization and native performance reporting; semantic
admission does not depend on that performance class.

Guard-page mode is available under `-tags wago_guardpage`. Its Linux/RISC-V
signal handler validates the active reservation and saved `S9`, distinguishes
in-range lazy growth from true out-of-bounds access, commits grown pages with
`mprotect`, writes the wasm trap for genuine OOB faults, and rewrites the saved
PC to the native trap exit. The handler's `ucontext` offsets are compile-time
checked against Go mirrors of the Linux/RV64 signal layout.

## Reproducible qualification

Run the checked-in qualification gate from an amd64 development host with
QEMU user emulation:

```bash
GO=/path/to/go QEMU_RISCV64=/usr/bin/qemu-riscv64 \
  scripts/riscv64-qualify.sh qemu
```

The QEMU mode cross-builds explicit and guard-page backend/runtime/public test
binaries, runs them with RVV, compressed instructions, Zba/Zbb/Zbc/Zbs, Zicond,
Zfa, Zfh, and Zacas disabled, then runs positive RVV detection and execution on
the default vector-capable model. If `WAGO_SPECTEST_DIR` and `wast2json` are
available, it also runs the official SIMD and relaxed-SIMD suites in both bounds
modes. Artifacts go under `.validation/riscv64-qualify` by default.

On real Linux/RV64 hardware, run:

```bash
STRESS_COUNT=20 BENCH_TIME=250ms BENCH_COUNT=5 \
  scripts/riscv64-qualify.sh native
```

Native mode records the kernel, Go version, commit, and `/proc/cpuinfo`; runs the
full explicit/guard test suites and repeated runtime, signal, cancellation, host,
and SIMD stress; optionally runs the official proposal suites; and records
explicit/guard corpus benchmarks. Performance or cross-hart publication claims
must attach these native artifacts. QEMU results remain correctness evidence
only.

## Delivery gates

Completed:

1. Encoder goldens and randomized immediate/patch tests.
2. Foreign-stack no-cgo runtime execution under QEMU.
3. Minimal integer/control railshot beachhead.
4. Production wrapper ABI, explicit traps, memory, calls, and scalar FP.
5. Synchronous host calls, tables, references, bulk memory, and public runtime
   integration.
6. Scalar corpus compile coverage and end-to-end result parity with amd64.
7. Linux/RISC-V guard-page execution, lazy growth, reservation reuse, and public
   runtime tests under QEMU.
8. Complete RV64G SWAR lowering for all 256 core/relaxed SIMD instructions,
   including pair allocation/spilling, memory atomicity, globals, control, and
   serialized direct/indirect/host/cross-instance ABIs.
9. Official SIMD and relaxed-SIMD proposal execution under QEMU with zero SIMD
   failures; multi-memory remains separately unsupported project-wide.
10. A checked-in QEMU/native qualification gate covering baseline-extension
    disablement, positive/negative RVV policy, both bounds modes, proposal suites,
    exhaustive v128 misalignment, cross-thread code publication stress, host
    metadata, and native benchmark artifacts.

Remaining:

11. Optional RVV lowering, only after native measurements justify a vector
    representation and tier-selection policy over the measured SWAR baseline.
12. Native-hardware guard-page, cross-hart publication, misaligned-access,
    correctness, code-size, memory, and performance measurements using the
    qualification gate above.
