# RV64 port contract

This experimental Linux/RV64 port has an encoder, an isolated integer runtime
boundary, shared scalar compilation, bounded whole-function integer/control
lowering, single-memory/integer-global execution, bulk memory/passive data and
defined same-instance direct/recursive integer calls.
Generated code executes under QEMU; see the
[direct-call checkpoint](riscv64-direct-call-checkpoint.md),
[bulk checkpoint](riscv64-bulk-checkpoint.md),
[memory checkpoint](riscv64-memory-checkpoint.md),
[integer checkpoint](riscv64-integer-checkpoint.md),
[scalar checkpoint](riscv64-scalar-checkpoint.md) and
[runtime checkpoint](riscv64-runtime-checkpoint.md).
It does not enable a production RV64 compiler, CLI target, artifact architecture,
or Wasm feature. The [encoder checkpoint](riscv64-encoder-checkpoint.md) records
the earlier encoder-only acceptance.

## Baseline and provenance

The fresh branch starts at `03a2db03ac06bcf20d5411272a7dce80a2138fae`,
the squash merge of #802 on 2026-10-04 at 21:48:05 UTC. The merged tree is
authoritative. The approved implementation plan is
`Wago-RISC-V64-Implementation-Plan.docx`, Library version 1; its 56,721 bytes
were materialized and read before implementation.

Current main has no `AGENTS.md`, `CLAUDE.md`, or tracked `.agents`/`.codex`
skills. `CONTRIBUTING.md`, `CONTEXT.md`, `README.md`, `ARCHITECTURE.md`,
`FEATURES.md`, and `ROADMAP.md` govern this checkpoint. The original dirty Wago
checkout and its active worktrees were left unchanged; the new worktree uses
a separate local object-sharing clone. No code or commits were copied from
historical #284. Open #284/#285 and checker work #798/#799/#800 are separate;
none was modified. The checker enhancements are not assumed to be merged.

The later memory checkpoint records the readonly main refresh after #799 and
the separate open #803; those changes were checked without modifying either PR.

## Merged shared contract

`src/core/compiler/backend/railshot/shared/scalar.go` has these 19 hooks:

| Hook | RV64 obligation for the later adapter |
| --- | --- |
| `Registers` | Physical IDs, fixed scratch and ABI registers excluded. |
| `ParameterRegister` | Begin all-false, after framed entry stores parameter homes. |
| `LocalOffset` | Checked, disjoint 8-byte local slots. |
| `SpillOffset` | Checked, disjoint spill/agreement slots. |
| `Constant` | Full i64; zero-normalized low-word i32. |
| `Move` | Copy i64; zero-normalize i32, including same-register moves. |
| `NormalizeI32` | Zero the upper 32 bits without optional ISA extensions. |
| `Load` | LD/LWU with reserved-scratch large-offset handling. |
| `Store` | SD/SW with reserved-scratch large-offset handling. |
| `Clobbers` | Audit fixed-register use in every sequence. |
| `Operand` | Begin register-only. |
| `Binary` | Preserve aliasing and Wasm width, including comparisons. |
| `ScaledAdd` | Begin false. |
| `BranchZero` | Test only the low word of raw i32 carriers. |
| `BranchCompare` | Inverse relation for the false edge, width-correct operands. |
| `Jump` | Fixed-size envelope using permanently reserved scratch. |
| `Patch` | Byte offsets; checked reach and alignment; no late relaxation. |
| `Position` | Byte offset in the current image. |
| `Return` | Normalize i32, marshal guest result, retain framed high-water mark. |

Shared code owns logical values, aliases, registers, spills and structured
agreements. The RV64 adapter uses those allocators unchanged. The merged
admission includes a cost heuristic that rejects some tiny multi-argument
leaves; fixtures must establish actual admission. Parallel move scheduling is
separate from `ScalarTarget` and needs a reserved-scratch cycle-safe RV64 swap.

RV64 word arithmetic sign-extends. Logical i32 results must instead be
zero-normalized. Raw incoming upper bits must be handled in arithmetic,
bitwise operations, comparisons, shifts, zero/compare branches, stores,
joins and returns. Signed comparisons must sign-extend low words into scratch
without destroying live aliases. Normalizing only direct returns is insufficient.

## Encoder scope

The encoder emits fixed 32-bit little-endian RV64I/M instructions. It provides
raw sign-extending word instructions and an explicit zero-extension sequence;
raw instructions do not by themselves implement a Wasm i32 operation.
Constants use at most eight instructions without temporary registers.
All operands are validated; an emission error is sticky and must be checked
before consuming the image. Invalid operands add no bytes.

Register names are physical `X0` through `X31`. The reservation mask protects
zero, RA, SP, GP, TP, FP, physical Go CTXT/X26 and g/X27, provisionally reserved
guest context/memory X24/X25, and both scratch registers X30/X31. It is a
starting reservation contract, not a completed guest ABI or allocator.
Long transfer envelopes clobber X31 only.

Short branches/JAL and targets require four-byte alignment because compressed
instructions are disabled. Long jumps/calls occupy eight bytes; conditional
envelopes occupy twelve and invert the condition over the long transfer.
Their patch site is the AUIPC instruction, including for conditional envelopes.
The exact four-byte-aligned AUIPC/JALR displacement interval is
`[-2147485696, 2147481596]`, with signed high-20/low-12 rounding. Out-of-range
targets fail without modifying bytes; fixups can be repeated or relocated
with image concatenation. No absolute pointers or image addresses are retained.

Primary encoding references:
[RV64I](https://docs.riscv.org/reference/isa/v20260120/unpriv/rv64.html),
[RV32I formats and control flow](https://docs.riscv.org/reference/isa/v20260120/unpriv/rv32.html),
[M](https://docs.riscv.org/reference/isa/v20260120/unpriv/m-st-ext.html).
The encoder emits a subset of the planned RV64G baseline. C, V, Zba, Zbb and
other optional extensions are off. Repository Go minimum remains 1.22;
toolchain and host CPU requirements must be qualified separately from JIT ISA.

## Next boundaries

The runtime stage must reconcile `src/core/runtime/code_buffer.go` growth,
`Seal`/`Take` ownership, `engine.go` publication, compiler-owned `codeimage`
transfers, thunks and artifact loading. Current `mem_linux.go` is limited to
AMD64/ARM64. Do not widen architecture tags on signal/assembly code blindly.
Use RW-to-RX W^X, process-wide Linux `riscv_flush_icache` with flags zero before
callable visibility, error cleanup and invocation-quiescent unmapping.

Begin framed with 16-byte SP alignment and stack bounds checked before frame
writes. Go entry, guest calls and Linux psABI are separate contracts. Sentinel
tests must preserve GP, TP, CTXT, g, saved registers, RA and original SP.
Current `hostcall_layout.go` has extended argument/result areas up to 65,535
slots; historical fixed host offsets cannot be reused without layout assertions.
Artifact format/version and architecture IDs remain current-main contracts;
no RV64 artifact ID is added by this checkpoint.

Before the shared-scalar milestone publishes any executable code, a whole-module
allowlist must admit every defined scalar function/export and only explicitly
inert metadata. Reject unsupported imports, start functions, memory, tables,
globals, initializers, types and features. Unsupported shared functions cannot
fall back to AMD64/ARM64 bytes. Later stages require genuine whole-function
RV64 lowering. No Core/Release parity or native performance is claimed.

The encoder checkpoint requires host-independent goldens, independent assembler
comparison, constant/immediate properties, fixup overflow/alignment tests and
existing-target compatibility. Runtime changes follow a separate independent
review. QEMU can qualify correctness only; native multicore cache/lifetime and
performance qualification remain required for a supported release.
