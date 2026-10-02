# General ARM64 code-generation research for Dragline

Research date: 2026-09-02  
Repository snapshot: `3540fd8b56bf121ea9f6acd40489404e197a6058`

## Scope

This note covers reusable ARM64 WebAssembly JIT mechanisms: typed SIMD/FP
residency, private-call contracts, memory instruction selection, control-flow
layout, and measurement on Apple silicon. It deliberately excludes module,
export, function-index, body-hash, corpus, and algorithm recognition. A change
is admissible only when its proof is stated in terms of WebAssembly semantics,
machine dataflow, and verified target/ABI facts.

## Ranked findings

### 1. Make V128 call preservation exact per physical register

This is the highest-leverage near-term direction for mixed structured/RailMach
code. A WebAssembly `v128` is a full 128-bit value, not a value whose upper half
may be discarded. ([WebAssembly value syntax](https://webassembly.github.io/spec/core/syntax/values.html#vectors))

AAPCS64 does **not** preserve V24-V31. V0-V7 and V16-V31 are caller-saved;
V8-V15 are callee-saved only in their bottom 64 bits, leaving preservation of a
larger value to the caller. V0-V7 are also the vector/FP parameter and result
registers. ([AAPCS64 SIMD and floating-point registers](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst#simd-and-floating-point-registers),
[AAPCS64 parameter passing](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst#parameter-passing))

Cranelift treats every vector register as caller-saved at ordinary AArch64 call
sites because its allocator cannot represent the partial V8-V15 preservation;
its `PreserveAll` convention instead saves the complete 128-bit register.
([Cranelift AArch64 ABI implementation](https://github.com/bytecodealliance/wasmtime/blob/main/cranelift/codegen/src/isa/aarch64/abi.rs))

The safe Dragline opportunity is more precise than either blanket policy:

- for a verified direct internal call, retain each live `v128` only when the
  callee contract proves that exact allocated FPR is not clobbered, or proves a
  full-Q save and restore;
- propagate nested-call clobbers into that contract;
- treat imports, indirect calls, unknown contracts, and public-ABI calls as
  conservative boundaries; and
- never infer preservation from a physical range such as V24-V31.

This uses Dragline's existing `ABIContract.FPRClobbers`/`CalleeFPRs` seam rather
than a new workload-specific path. The verifier should reject a contract that
claims a 128-bit value survives through an 8-byte ABI save.

### 2. Keep FP and SIMD values typed and resident end to end

AArch64 has a shared SIMD/floating-point register bank, while WebAssembly
defines vectors as native 128-bit numeric values. ([AAPCS64 machine registers](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst#machine-registers),
[WebAssembly vectors](https://webassembly.github.io/spec/core/syntax/values.html#vectors))
Consequently, repeated FP/SIMD-to-GPR moves and frame round trips are
representation overhead, not a semantic requirement.

The general target is typed FPR residency across operand-stack values, block
arguments, local values, spills/reloads, and private calls. Coalescing should
prefer the destination register required by the next vector instruction or
call result. Bitcasts should remain no-code aliases when the allocation and the
consumer permit it. Cross-bank moves remain necessary only at an actual ABI or
instruction constraint.

Measure this with target-independent counters rather than corpus names:
cross-bank moves, Q spills/reloads, fixed-register repairs, and live-range
splits per executed hot loop. Any optimization must also handle arbitrary
legal lane values and all supported vector operations.

### 3. Extend bounded memory selection around existing semantics proofs

LLVM's current AArch64 load/store optimizer is a useful primary-source model
for a bounded local pass: it pairs compatible accesses, folds pre/post-index
updates and constant offsets, checks alignment and interference, optionally
renames registers to expose pairs, and folds `UMOV` plus a GPR store into a
direct FPR store. Its searches have explicit limits rather than unbounded
whole-function analysis. ([LLVM AArch64 load/store optimizer](https://github.com/llvm/llvm-project/blob/main/llvm/lib/Target/AArch64/AArch64LoadStoreOptimizer.cpp))
LLVM's AArch64 post-RA scheduler separately reorders non-overlapping,
same-base stores by offset, demonstrating that selection and scheduling are
distinct general mechanisms. ([LLVM AArch64 machine scheduler](https://github.com/llvm/llvm-project/blob/main/llvm/lib/Target/AArch64/AArch64MachineScheduler.cpp))

Dragline already has pair and pre/post-index rewrites, so the next general work
should improve their inputs and coverage rather than add another isolated
matcher:

- schedule independent compatible accesses adjacently within a bounded window;
- fold address increments only when the updated base has the same SSA meaning;
- form vector loads/stores directly in FPRs instead of bouncing through GPRs;
- combine `base + offset` and `base + extended/scaled index` trees into legal
  target addressing modes (wazero's ARM64 lowerer applies this generally, while
  documenting remaining shifted-index coverage); and
- track aliases and ordered side effects explicitly so a candidate can be
  rejected cheaply.

([wazero ARM64 memory lowering](https://github.com/tetratelabs/wazero/blob/f4779551afb474c7f2ac79929ce2b3390197544c/internal/engine/wazevo/backend/isa/arm64/lower_mem.go#L270-L418))

The semantic guard is strict. WebAssembly allows unaligned accesses regardless
of the alignment hint, and a load traps when its exact access width is out of
bounds. ([WebAssembly memory execution](https://webassembly.github.io/spec/core/exec/instructions.html#memory-instructions))
Do not widen a memory access, move it across an observable side effect, or
change which access traps first. In particular, store pairing needs a proof
that partial architectural stores cannot expose behavior forbidden by Wasm;
otherwise retain separate stores.

### 4. Make hot fallthrough and cold traps a generic layout property

Cranelift's single-pass machine buffer records branch fixups and then applies
small general rules: remove branches to fallthrough, thread empty jump blocks,
invert conditional-plus-unconditional pairs when that creates fallthrough, and
insert veneers only when branch range requires them. Its source also records
the AArch64 reach distinction between conditional and unconditional branches.
([Cranelift machine-code buffer](https://github.com/bytecodealliance/wasmtime/blob/main/cranelift/codegen/src/machinst/buffer.rs))

The corresponding Dragline direction is a bounded block-layout/fixup stage,
not recognition of a hot algorithm:

- order the likely loop continuation as fallthrough;
- place trap, bounds-failure, and other uncommon exits after the hot region;
- remove branch-to-next and jump-only blocks;
- invert conditions when it removes an unconditional branch; and
- retain an explicit veneer/far-branch fallback so layout never compromises
  correctness.

Keep NZCV-producing compares adjacent to their branch, trap, `CSEL`, or `FCSEL`
consumer when possible, instead of materializing a boolean into a GPR and
comparing it again. Both Cranelift and wazero contain general ARM64 lowering
paths of this kind. ([Cranelift AArch64 conditional lowering](https://github.com/bytecodealliance/wasmtime/blob/bf330493f4352546ee2a3435eeb85d75d6328f1b/cranelift/codegen/src/isa/aarch64/inst.isle#L4889-L4925),
[wazero ARM64 conditional lowering](https://github.com/tetratelabs/wazero/blob/f4779551afb474c7f2ac79929ce2b3390197544c/internal/engine/wazevo/backend/isa/arm64/lower_instr.go#L2040-L2089))

This should reduce both dynamic branches and hot native bytes. It is especially
valuable when Apple CPU Counters attributes time to instruction-delivery or
discarded-work bottlenecks, rather than being enabled because a named corpus
benefited. Wazero's current ARM64 backend provides another primary-source
example: it collects trap exits and emits shared trap sequences after the
function body rather than duplicating the full nonreturning path inline.
([wazero ARM64 machine](https://github.com/tetratelabs/wazero/blob/main/internal/engine/wazevo/backend/isa/arm64/machine.go),
[wazero ARM64 prologue/epilogue and exits](https://github.com/tetratelabs/wazero/blob/main/internal/engine/wazevo/backend/isa/arm64/machine_pro_epi_logue.go))

### 5. Reduce call overhead through a verified private ABI, not public-ABI assumptions

AAPCS64 assigns X0-X7 and V0-V7 to arguments/results, X19-X28 to preserved
integer state, and X16/X17 to intra-procedure-call scratch that linkers may use
for veneers. X18 is platform-specific. ([AAPCS64 general-purpose registers](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst#general-purpose-registers))
Apple reserves X18 and permits leaf functions and tail calls to omit a frame
record. ([Apple ARM64 ABI guidance](https://developer.apple.com/documentation/xcode/writing-arm64-code-for-apple-platforms))
AAPCS64 additionally requires SP to remain 16-byte aligned at public interfaces
and whenever memory is accessed through SP. ([AAPCS64 stack constraints](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst#the-stack))

General call reductions therefore include direct internal branches, register
arguments/results, destination coalescing, leaf frame omission, tail calls when
Wasm semantics permit them, and saves restricted to the verified clobber set.
Keep X16/X17 available for long-call machinery and never allocate X18 on Apple
platforms. Public, imported, indirect, and host calls must retain their full
platform/runtime boundary behavior.

## Apple-silicon measurement protocol

Apple recommends a measure-change-remeasure loop on the affected device. Its
CPU Counters instrument separates instruction-delivery, instruction-processing,
and discarded-work bottlenecks and can direct a follow-up recording to more
specific counters. Time Profiler supplies the call-tree context.
([Addressing CPU bottlenecks](https://developer.apple.com/documentation/xcode/addressing-cpu-bottlenecks),
[Improving app performance](https://developer.apple.com/documentation/xcode/improving-your-app-s-performance))

For supported recent Apple silicon, Processor Trace records the actual branch
and call stream with low hardware tracing overhead; Instruments 16.3 or later
supports recording on M4-or-later Macs running macOS 15.4 or later. Apple notes
that the processor-trace mechanism itself typically slows the device by less
than 1%, while the complete Instruments session may add more overhead.
([Processor Trace instrument](https://developer.apple.com/documentation/xcode/analyzing-cpu-usage-with-processor-trace))

Use the following gate for each candidate:

1. Run alternating before/after samples on the same machine, power state,
   affinity/concurrency, and benchmark binary provenance; reject thermally or
   scheduler-contaminated samples.
2. Record execution latency, native bytes, compile latency, compile RSS, and
   allocations. A runtime win that merely transfers excessive cost to compile
   time or code size is not automatically acceptable.
3. Attribute the change with CPU Counters: instruction delivery suggests code
   size/layout, discarded work suggests branch behavior, and instruction
   processing calls for instruction/dependency/memory inspection.
4. Use Processor Trace for call/branch frequency and map raw JIT PCs back to
   Dragline's emitted function ranges. This mapping requirement is an inference:
   dynamically emitted code may not have ordinary dSYM symbols, and Apple notes
   that some generated branch islands appear as raw addresses even when symbols
   are supplied.
5. Run differential correctness with randomized, shape-varied Wasm inputs and
   the full non-ISA corpus. The optimization gate is an IR/dataflow predicate,
   never a named workload.

Arm core-specific optimization guides can suggest experiments such as paired
accesses, compare/branch adjacency, or shorter dependency chains, but their
pipeline widths and thresholds are not Apple-silicon contracts. Treat those as
hypotheses and let the Apple hardware counters decide whether to retain them.
([Arm Cortex-A77 Software Optimization Guide](https://developer.arm.com/documentation/swog011050/c))

## Recommended implementation order

1. Per-physical-FPR, full-128-bit call-preservation proof for direct internal
   calls, with conservative unknown-call fallback.
2. Typed FP/V128 residency through block arguments and private-call lowering,
   measured by eliminated cross-bank moves and Q spills.
3. Bounded memory scheduling/selection that exposes existing pair and
   pre/post-index machinery, including direct FPR stores.
4. Generic fallthrough/cold-trap layout and branch threading.
5. Broader leaf/tail/direct-call frame and save elimination under the same
   verified ABI contracts.

Each item is reusable across arbitrary Wasm modules and should be retained only
when paired measurements show a general non-ISA improvement without semantic,
compile-memory, or native-size regressions outside the agreed gates.

One tempting transformation remains out of scope without an explicit semantic
proof: ordinary WebAssembly multiply followed by add cannot be replaced by
`FMADD`/`FMSUB`, because fused evaluation changes rounding. Cranelift limits
fused ARM64 lowering to IR operations that already specify fused semantics.
([Cranelift AArch64 fused-operation lowering](https://github.com/bytecodealliance/wasmtime/blob/bf330493f4352546ee2a3435eeb85d75d6328f1b/cranelift/codegen/src/isa/aarch64/lower.isle#L606-L675))
