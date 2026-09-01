# Dragline ARM64 execution research

Research snapshot: 2026-09-01. Upstream source comparisons are pinned to
Wasmtime `e6a948e3` and regalloc2 `2fe490bc`. This report deliberately makes no
benchmark claim: each item is a source-backed hypothesis that still needs a
paired corpus measurement on the target machine.

## Conclusion

The largest visible architectural gap is not an isolated ARM64 peephole. It is
that Dragline's optimizing RailMach path has no `v128` machine type, register
class, spill contract, or private-call contract. SIMD values therefore cannot
participate in that optimizer as first-class values; relevant functions remain
on or fall back to the structured emitter, outside the scheduler, block layout,
typed register allocator, and interprocedural ABI work already built for scalar code.
Cranelift instead puts every vector up to 128 bits in its FP/vector register
class and sends it through the same VCode and register-allocation pipeline as
scalar values ([AArch64 type classes](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/isa/aarch64/inst/mod.rs#L1140-L1152),
[code-generation pipeline](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/machinst/compile.rs#L15-L78)).

The best path toward a large application-level gain is therefore:

1. make `v128` a first-class RailMach value;
2. split and coalesce live ranges around calls and hot loops;
3. canonicalize and hoist explicit bounds checks;
4. finish the private typed ABI for every eligible internal edge;
5. add critical-path scheduling and final branch cleanup.

These changes reinforce one another. SIMD residency is of limited value if a
call or block edge immediately spills it, and a better scheduler is of limited
value if register pressure converts its latency win into frame traffic.

## Prioritized work

### P0: first-class `v128` in RailMach

Current Dragline evidence:

- [`MachineType`](../src/core/compiler/backend/dragline/railmach/mach.go) ends at
  scalar `TypeF64`/`TypeRef`; `machineType` rejects `v128`.
- [`railMachCandidate`](../src/core/compiler/backend/dragline/production_plan.go)
  explicitly retains mixed SIMD functions on the structured emitter until a
  128-bit register and spill contract exists.
- The structured ARM64 emitter manages bounded, hand-selected V-register sets
  for vector stack values and locals in
  [`compile_arm64.go`](../src/core/compiler/backend/dragline/compile_arm64.go),
  outside RailMach's allocator; RailMach's scalar FP values already remain in
  its typed FPR bank.

Implement one typed `v128` value occupying one ARM64 V register, with 16-byte
spill slots and alignment, edge moves, block arguments, fixed operands,
rematerialization, call arguments/results, and verifier coverage. Admit SIMD
functions to the ordinary RailSSA/RailMach selection, scheduling, allocation,
post-RA, and layout pipeline only as each operation has complete lowering.

Then add tree selection before generic fallbacks. Cranelift recognizes broadcast,
concatenation, and fixed shuffle shapes as `DUP`, `EXT`, `ZIP`, `UZP`, `TRN`, or
`REV` before falling back to table lookup
([shuffle rules](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/isa/aarch64/lower.isle#L159-L280)).
It also recognizes widening pairwise reductions, feature-gated dot products,
and element-form FMLA for splat operands
([reduction rules](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/isa/aarch64/lower.isle#L372-L430),
[FMLA rules](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/isa/aarch64/lower.isle#L643-L680)).
Only fuse arithmetic when WebAssembly permits the resulting rounding behavior.

Proof gate: the target SIMD corpora must show lower vector spill/reload counts,
fewer GPR/FPR transfer instructions, and lower paired execution latency without
changing differential results.

### P0: conflict-driven live-range splitting and move elimination

Dragline's RAGreedyP starts from a complete linear allocation, then promotes,
evicts, assigns callee-saved regions, and creates selected regional fragments
([`ragreedyp.go`](../src/core/compiler/backend/dragline/railmach/ragreedyp.go)).
That is a useful base, but it does not yet provide arbitrary conflict-driven
splitting and coalescing across the whole machine SSA graph.

regalloc2's optimizing allocator weights uses, merges reused-input/output and
block-parameter bundles, then chooses eviction or a split at the first actual
conflict. It trims no-use range tails into a shared spill bundle so values are
stored after their last use and reloaded before their next use, and runs a
redundant spill/load elimination pass afterward
([weighted ranges](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/ION.md#L80-L115),
[bundle merging](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/ION.md#L335-L405),
[conflict-driven splitting](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/ION.md#L536-L641),
[redundant spill elimination](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/ION.md#L903-L940)).

Deepen RAGreedyP rather than replacing it wholesale:

- represent multiple fragments for every GPR/FPR/vector live range;
- split at fixed-register conflicts, calls, loop boundaries, and pressure peaks;
- weight uses by loop/block frequency, not merely range length;
- coalesce reused operands and block-argument edge moves before allocation;
- keep cold/no-use portions in a canonical home, with second-chance allocation;
- eliminate redundant moves after parallel-copy resolution;
- prefer caller-saved registers for short leaf ranges and use callee-saved
  registers only when weighted survival across calls repays the prologue cost.

Proof gate: per hot function, report weighted spill debt, dynamic stack-relative
operations, edge moves, callee-save bytes, and latency. A lower spill-slot count
alone is insufficient.

### P1: canonical bounds certificates and loop-range checks

Cranelift does more than choose between “check” and “no check.” It checks a whole
object once to enable GVN across field accesses, removes static checks, uses
reservation/guard invariants, and normalizes guard-covered accesses with
different static offsets to the same `index > bound` expression so GVN can
deduplicate them
([current bounds implementation](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/crates/cranelift/src/bounds_checks.rs#L72-L165),
[guard and GVN fast paths](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/crates/cranelift/src/bounds_checks.rs#L265-L325),
[offset normalization](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/crates/cranelift/src/bounds_checks.rs#L400-L435)).
Its official execution guide also treats guard-page enforcement as the normal
way to omit explicit checks when reservation and offset constraints permit it
([Wasmtime guide](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/docs/examples-fast-execution.md#L20-L48)).

Extend Dragline's existing bounds certificates to a canonical tuple such as
`(memory, index SSA value, maximum covered end, memory epoch)`. One dominating
certificate should cover smaller field offsets and repeated loop accesses.
Normalize guard-covered offsets to one expression; hoist invariant maximum-end
checks into loop preheaders; invalidate dynamic-length facts at `memory.grow`
and calls that can grow memory. Guard-reservation facts may survive only when
the runtime invariant proves that safe.

The check and the load/store must consume one authoritative effective-address
value. A 2026 Cranelift ARM64 sandbox escape was caused by the checked address
and accessed address diverging
([advisory](https://github.com/bytecodealliance/wasmtime/security/advisories/GHSA-jhxm-h53p-jm7w)).
Every transformation must retain the specified out-of-bounds trap
([WebAssembly memory semantics](https://webassembly.github.io/spec/core/exec/instructions.html#memory-instructions)).

Proof gate: count dynamic comparisons and memory-length loads per loop, then run
guard-page, explicit-check, overflow, memory-grow, and differential trap tests.

### P1: finish call-site-aware typed internal calls

Cranelift uses a private internal Wasm calling convention, carries parameter and
result types into the signature, and uses its tail-call convention even when the
Wasm tail-call proposal is disabled
([internal signature](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/crates/cranelift/src/lib.rs#L187-L222)).
On ARM64 it has separate integer and vector argument/result streams and can
return values in both classes
([AArch64 ABI lowering](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/isa/aarch64/abi.rs#L178-L201)).
This follows the architecture's native split between GPR and SIMD/FP parameter
registers
([AAPCS64](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst#parameter-passing)).

Dragline already has a stronger-than-generic private scalar ABI and exact
callee contracts. Complete it rather than adding another ABI:

- carry `v128`, FP, multi-value, and block-edge values in their native banks;
- keep same-instance context, memory base, and stable bounds state pinned across
  eligible local calls;
- use direct `BL` for linked same-module calls and reserve address materialization
  plus `BLR` for imports and genuine indirect calls;
- specialize typed/non-null immutable table entries to remove null/signature
  guards and directize the target where proven;
- split only values actually live across a call and use the callee's exact
  clobber mask;
- lower `call; return` to a sibling/tail branch when signatures, roots, frame,
  and trap state permit it.

Proof gate: report per-call argument/result moves, call-area loads/stores,
context/bounds reloads, saved registers, and direct versus indirect branch
counts.

### P1: critical-path and pressure-aware ARM64 scheduling

Dragline's `ScheduleKindLatencyFusion` prioritizes an instruction's own latency
and resource cost; it does not rank the remaining dependency-chain height
([`schedule.go`](../src/core/compiler/backend/dragline/railmach/schedule.go)).
That can issue a locally expensive node while leaving a longer load or FP chain
blocked.

Use bounded list scheduling inside hot straight-line regions. Compute reverse
critical-path height from the verified dependency DAG, then choose ready nodes
by critical height, target latency/throughput, memory dependency, fusion
opportunity, and incremental register pressure. Preserve trap order, aliasing,
flags, calls, and safepoints. Apple publishes M-series instruction latency,
bandwidth, microarchitecture, and counter tables specifically for this purpose
([Apple Silicon CPU Optimization Guide](https://developer.apple.com/documentation/Apple-Silicon/cpu-optimization-guide)).
Cranelift itself has an open first-party proposal for critical-path/list
scheduling rather than arbitrary topological order
([Cranelift issue #6260](https://github.com/bytecodealliance/wasmtime/issues/6260));
a bounded target scheduler is therefore also an opportunity to beat, not merely
copy, Cranelift.

Run scheduling before final allocation or score its predicted pressure and
reject candidates that add weighted spill debt. Start with independent
load/address/arithmetic chains found in the worst measured functions.

Proof gate: hardware counters must show reduced dependency stalls or cycles with
no increase in dynamic spill/reload traffic.

### P2: emission-time cold sinking and branch cleanup

Dragline has a deterministic ExtTSP-like chain layout, but it explicitly performs
no control-flow rewrite
([`layout.go`](../src/core/compiler/backend/dragline/railmach/layout.go)).
Cranelift sinks all marked cold blocks during final emission
([VCode emission](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/machinst/vcode.rs#L748-L770))
and its machine buffer removes fallthrough branches, threads empty blocks, and
inverts conditional/unconditional pairs after actual offsets are known
([MachBuffer](https://github.com/bytecodealliance/wasmtime/blob/e6a948e32fb4376dfd7029c7fa6ac7c5c82a196d/cranelift/codegen/src/machinst/buffer.rs#L62-L106)).

After allocation and final byte sizing, sink trap bodies, failed indirect-call
guards, slow bulk-memory paths, and other zero/rare-weight blocks. Choose the hot
successor as fallthrough, invert the condition when needed, thread branch-only
blocks, and delete jumps to the next instruction. Keep loop headers and hot
bodies contiguous; align only headers whose measured fetch behavior improves.

Proof gate: report hot bytes, taken/unconditional branch counts, front-end
stalls, and code size. Retain a layout change only when paired execution improves.

## Routine research and measurement loop

Use the same loop for each non-ISA corpus rather than accumulating speculative
peepholes:

1. Run alternating, paired Dragline/baseline samples with fixed inputs and
   randomized engine order; retain raw rounds.
2. Attribute the hottest function with disassembly and Apple hardware counters:
   retired instructions, cycles, branch misses, load/store traffic, cache/TLB
   misses, and dependency/front-end stalls where available.
3. Record a static ledger for that function: native bytes, calls, branches,
   explicit bounds checks, memory-length loads, GPR/FPR/vector transfers,
   spills/reloads, and callee saves.
4. Select one mechanism above that explains both the dynamic and static signal.
5. Re-run focused correctness and differential traps, then the paired focused
   benchmark. Revert neutral or regressing changes.
6. Periodically rerun the complete non-ISA corpus to detect displaced costs;
   do not generalize a focused win until the aggregate and worst case agree.
7. Refresh these upstream references before implementing them. Cranelift and the
   Apple optimization guide are active sources; source shape, security rules,
   and microarchitecture tables can change.

The completion criterion is not “all five mechanisms exist.” It is a current,
reproducible full-corpus result meeting the requested execution target, with
correct Wasm traps, no corpus regressions hidden by an average, and compile RSS,
compile latency, and code size still reported alongside execution latency.
