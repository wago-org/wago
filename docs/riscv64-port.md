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
  operations, structured control, direct/indirect calls, explicit-bounds memory,
  bulk memory, globals, tables, references, traps, and wrapper/internal ABIs.
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
- RVV/SIMD, full Release 2 execution including SIMD, and native-hardware
  benchmarking remain deferred. SIMD modules are rejected explicitly before
  scalar code generation.

## Target baseline

The scalar baseline is RV64G as exposed by Go's `linux/riscv64` target: RV64I
plus M, A, F, D, Zicsr, and Zifencei. Generated scalar code does not require the
compressed extension. The current backend always omits SIMD from
`SupportedFeatures` and rejects SIMD modules. A future RVV backend may admit SIMD
only after detecting the V extension and a VLEN of at least 128 bits; it must not
silently scalarize an incomplete subset.

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
unaligned accesses; correctness must not depend on a platform silently emulating
misaligned scalar loads. The lowering may use direct loads only when the access
is proven naturally aligned or the supported Linux/hardware baseline explicitly
guarantees the required behavior. Otherwise it must synthesize the access.

Guard-page mode is available under `-tags wago_guardpage`. Its Linux/RISC-V
signal handler validates the active reservation and saved `S9`, distinguishes
in-range lazy growth from true out-of-bounds access, commits grown pages with
`mprotect`, writes the wasm trap for genuine OOB faults, and rewrites the saved
PC to the native trap exit. The handler's `ucontext` offsets are compile-time
checked against Go mirrors of the Linux/RV64 signal layout.

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

Remaining:

8. Full Release 2 execution after RVV removes the intentional SIMD gap.
9. RVV lowering plus SIMD/relaxed-SIMD corpus parity.
10. Native-hardware guard-page stress, correctness, code-size, memory, and
    performance measurements.
