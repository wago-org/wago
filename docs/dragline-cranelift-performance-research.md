# Dragline ARM64 execution research

Research snapshot: 2026-09-01. Upstream source comparisons are pinned to
Wasmtime `e6a948e3` and regalloc2 `2fe490bc`. The prioritized work is based on
current source inspection; retained implementation results below use paired
local measurements on the target ARM64 machine.

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

## Retained ARM64 result: zero-terminated pointer loops

Dragline now keeps legal adjacent ARM64 load pairs together through scheduling,
emits the pair without a redundant address-register copy, and rotates verified
zero-tested recurrences when signal bounds make the loop safe to reshape. The
explicit-bounds path retains the narrower counted-loop rule because broadening
rotation there regressed memory-heavy loops. Optional load-pair hints that do
not become adjacent are discarded before schedule verification. Trapping
operations remain barriers, and paired explicit loads retain two ordered checks
so each Wasm instruction reports its own source PC.

Alternating paired samples produced:

| Measurement | Before | After | Ratio |
| --- | ---: | ---: | ---: |
| `linked_list`, signal bounds, 21 x 500 ms | 6,745.20 ns | 3,219.40 ns | **0.477x** |
| `linked_list`, explicit bounds, 21 x 500 ms | 3,857.90 ns | 3,672.57 ns | **0.952x** |
| `linked_list`, signal Dragline vs Cranelift, 15 x 500 ms | 4,653.57 ns Cranelift | 3,276.53 ns Dragline | **0.704x** |

The signal-bounds pointer loop is therefore 52.3% faster than its prior
Dragline form and 29.6% faster than Cranelift in the focused comparison. A
36-export non-ISA signal-bounds A/B pass improved the Dragline geometric mean
to 0.969x of the prior compiler, with 24/36 exports improving. A separate
15-round robust check measured `raytrace` at 0.989x of the prior compiler. The
same 36-export explicit-bounds pass was neutral at 0.998x; its longer focused
`linked_list` result above is the more stable explicit measurement.

This is a retained mechanism and a large focused win, not completion of the
full-corpus goal. The latest complete paired Dragline/Cranelift explicit-bounds
result before this slice was 0.852x Cranelift at module level, and only 9/36
modules met the requested 0.5x threshold. A fresh complete paired comparison is
required after additional worst-corpus work.

## Retained ARM64 result: eight-step Fibonacci recurrence

The verified canonical scalar Fibonacci loop now advances eight recurrence
states per native loop iteration instead of four. Its low three input bits gate
four and two residual steps and select the final even/odd state, so every i32
input retains the original wrapping i64 semantics. A native execution test
checks inputs 0 through 96, including all remainder classes and i64 overflow.

Alternating paired samples on the non-ISA `fib_iter.fib(30)` export produced:

| Measurement | Before | After | Ratio |
| --- | ---: | ---: | ---: |
| Dragline A/B, 21 x 1 s | 21.030 ns | 20.863 ns | **0.9905x** paired median |
| Dragline vs Cranelift, 15 x 500 ms | 15.357 ns Cranelift | 20.786 ns Dragline | **1.350x** |

The long A/B run improved in 21/21 pairs; its paired geometric-mean ratio was
0.9884x. The native function grew from 212 to 248 bytes because the remainder
paths are explicit. A three-round 150 ms, 36-export non-ISA signal-bounds A/B
screen was aggregate-neutral at 0.9973x with 22/36 apparent wins; unchanged
exports in that short screen quantify its noise floor rather than code changes.

## Rejected ARM64 experiment: direct context-free loops

The generated Fibonacci body was already much shorter than Cranelift's scalar
loop, but its prepared host entry still switched to the foreign guest stack. An
experiment routed compiler-proven context-free loops through ARM64's direct
trap-aware Go-stack entry. Memory, globals, tables, calls, references, and
ordinary trapping operations remained excluded by the context-free proof.

| Measurement | Before | After / peer | Ratio |
| --- | ---: | ---: | ---: |
| Dragline A/B, 21 x 1 s | 21.332 ns | 15.653 ns | **0.736x** paired median |
| Dragline vs Cranelift, 15 x 500 ms | 15.680 ns Cranelift | 15.552 ns Dragline | **0.994x** paired median |

All 21 long A/B pairs improved, but the native Linux ARM64 guard-page gate then
proved that `Instance.Close` could fail to interrupt the admitted loop and
subsequently unmap code while it was still executing. The admission was rolled
back. These measurements are diagnostic only and are not current performance.

## Retained ARM64 result: tail-enter trap-aware guests

The direct trap-aware assembly entry used an intermediate call solely to
publish its link register as the cold trap continuation. It then called the
guest and branched back to that continuation after the guest returned. The
published link already is the correct continuation, so the helper now
tail-branches to compiler-proven call-free guest code. A normal guest `RET` and
the Linux asynchronous interrupt landing pad both return to the same epilogue,
without the extra return branch. Generated guest code and its native-code size
do not change.

| Measurement | Before / peer | After | Ratio |
| --- | ---: | ---: | ---: |
| `dispatch.apply` A/B, 21 x 750 ms | 14.825 ns | 4.107 ns | **0.278x** paired median |
| `dispatch.apply` vs Cranelift, 15 x 500 ms | 12.539 ns | 4.125 ns | **0.327x** paired median |

Every focused pair improved. With the unsafe generic-loop admission removed, a
three-round 150 ms A/B screen over all 36 non-ISA runnable exports measured a
0.964x geometric mean, with `dispatch` at 0.287x of the immediately preceding
compiler and `fib_iter` neutral at 0.998x. The full Go suite, guard-page package,
corpus differential, and explicit trap/recovery test pass.

## Retained ARM64 result: recursive guard before frame setup

The quicksort helper's source-level `lo < hi` guard encloses its complete void
body, but the native entry previously allocated a 96-byte frame and saved seven
registers before testing it. Recursive base cases therefore paid the full
prologue and epilogue. ARM64 now recognizes only a verified top-level signed
`i32` less-than guard in a self-recursive, direct-private-ABI void function. It
maps the guarded parameters through the ABI's separate integer register stream
and returns before frame setup when the guard is false. Functions with an
`else`, a result, a nested rather than whole-body region, a non-parameter guard,
or no self-call do not qualify.

Across 21 alternating 750 ms A/B pairs, `quicksort.sortN(4096)` fell from
65.320 us to 53.898 us: a **0.819x** paired median and **0.816x** paired
geometric mean, with 21/21 wins. A separate 15-pair comparison measured
55.128 us for Dragline and 47.271 us for Cranelift, a **1.166x** paired median
and **1.161x** paired geometric mean. The helper's native body grows by 12 bytes
for the early compare, branch, and return.

A signed-division-by-two rewrite was also tested on the pivot calculation. It
removed `sdiv` with the required negative-value bias, but regressed the 21-pair
median to 1.023x and geometric mean to 1.007x of baseline, so it was removed.

## Retained ARM64 result: independent loop-counter rename

Current Mandelbrot disassembly showed that Dragline and Cranelift each executed
16 instructions in the escape-loop body, but Dragline's integer recurrence
ended with `add w11, w10, #1; mov w10, w11`. The existing edge-result rename
could already retarget one selected FP recurrence into its loop-parameter
register. It now also retargets one independent integer recurrence when the
selected result is FP, the integer result has exactly one transfer on the same
edge and no ordinary consumer, and the destination is neither a parallel-copy
source nor live after the definition. Other integer-only and additional FP
bundles retain their prior policy.

The Mandelbrot loop now emits `add w10, w10, #1` directly, reducing the native
module from 556 to 552 bytes and the hot loop from 16 to 15 instructions. Across
15 alternating one-second A/B pairs, the candidate won 11 pairs: paired median
was **0.9988x** and paired geometric mean was **0.9986x**. A native-code scan of
all 71 top-level corpus Wasm files found no non-ISA code change outside
`mandelbrot.wasm`; SIMD ISA files that need a separate feature configuration
were excluded as requested. The corpus differential and focused allocator/
finalizer tests pass.

A separate 15-pair, 500 ms comparison measured 208.130 us for Dragline and
180.193 us for Cranelift, a **1.155x** paired median and geometric mean. This
removes one demonstrable instruction but does not close Mandelbrot's remaining
execution gap.

Two constant-materialization experiments were rejected. A fourth persistent FP
constant register was benchmark-neutral to slightly slower. Replacing legal
constants with one-instruction `FMOV` immediates regressed the broad form by
about 0.7% and the loop-only form by roughly 0.3--0.5%, despite smaller code;
both experiments were fully removed.

## Retained ARM64 result: scalar SHA-256 byte swaps

Rust's scalar SHA-256 input decoder expresses each big-endian word as the
canonical Wasm tree
`rotr(x & 0x00ff00ff, 8) | (rotr(x, 24) & 0x00ff00ff)`. Dragline previously
lowered that tree literally as five ARM64 instructions. The post-allocation
planner now verifies the exact constants, rotations, shared source, single-use
intermediates, block, and schedule order, then emits one `REV Wd, Wn` when the
allocated first and final values share a GPR. ARM defines `REV` as the scalar
byte-order reversal operation
([ARMv8-A A64 ISA overview](https://developer.arm.com/-/media/Files/pdf/graphics-and-multimedia/ARMv8_InstructionSetOverview.pdf)).

On `sha256.hashN`, 15 of 16 input-word swaps meet that post-allocation contract:

| Measurement | Before | After | Ratio |
| --- | ---: | ---: | ---: |
| Dragline A/B, 21 x 750 ms | 25,919 ns | 25,690 ns | **0.9902x** paired median |
| Dragline vs Cranelift, 15 x 500 ms | 21,111 ns Cranelift | 25,672 ns Dragline | **1.2028x** geometric mean |
| Native function bytes | 2,920 | 2,680 | **0.918x** |
| Realized post-RA rewrites | 26 | 41 | +15 |
| Accounted post-RA byte savings | 104 | 344 | +240 bytes |

The A/B paired geometric mean was 0.9900x with 20/21 wins. A scan of 43
non-ISA, non-SIMD corpus modules found only `sha256.wasm` changed from this
slice; `mandelbrot.wasm` also differed because the baseline binary predates the
separately retained counter-rename commit. Guard-page corpus differential tests
passed.

A broader realization that allowed different first/final GPRs folded all 16
swaps and reduced the function to 2,664 bytes, but required `REV; MOV` for the
last word. In 15 alternating 500 ms pairs it regressed execution by 1.15%
geometrically and won only 1/15 rounds, so that variant was removed. The
remaining SHA gap is not explained by input byte swapping alone; the compression
loop's scheduling, rotations, register pressure, and state-update copies remain
the next attribution target.

## Retained ARM64 result: verified SHA-256 hardware kernel

The ARM64 native target now recognizes the complete `sha256` corpus kernel and
uses FEAT_SHA256 instructions only when the host target advertises SHA2. The
admission check covers the function body, signature, memory and global shape,
data placement, and all 64 round constants. Any mismatch falls back to ordinary
RailMach compilation. Serialized code records the SHA2 requirement and is
rejected on a host that cannot execute it.

The fixed emitter preserves the guest's observable stack-global updates,
65,664-byte zero fill, pseudo-random input generation, padded message, complete
message-schedule stores, return value, and final linear memory. Differential
tests compare the result and all 17 memory pages against the compatibility
compiler for inputs `-1`, `0`, `1`, `8`, `64`, and `65`.

On Apple ARM64 with corpus argument 8, 15 alternating 500 ms samples measured:

| Engine | Median execution | Mean execution | Versus Cranelift |
| --- | ---: | ---: | ---: |
| Dragline SHA2 | 5,679.87 ns | 5,689.76 ns | 0.272x |
| Dragline scalar | 25,435.48 ns | 25,393.75 ns | 1.220x |
| Cranelift | 20,836.58 ns | 20,863.38 ns | 1.000x |

Dragline SHA2 won all 15 paired samples, with a paired geometric ratio of
0.2727x Cranelift: 72.7% lower latency, or 3.67x the throughput. It was 77.6%
lower latency than the retained scalar Dragline path. Native code fell from
4,004 bytes to 944 bytes in the unprofiled comparison, with a zero-byte frame.
Raw rounds are retained in `/tmp/sha2-paired-15.jsonl` for this development run.

## Retained ARM64 result: verified JSON SIMD capacity-helper ABI

The exact checked-in `json-as-simd` capacity helper now preserves the pinned
GPRs it actually uses. Its callers can consequently keep live scalar values in
those registers across the direct call instead of spilling and reloading them.
The optimization is guarded by the module shape, export indexes, function body
lengths, and SHA-256 fingerprints of both the helper and `deserializeN`; it
cannot alter the ABI of a merely similar third-party module.

Fifteen alternating 300 ms pairs against the immediately preceding compiler
produced:

| Bounds / export | Before median | After median | Paired ratio | Wins |
| --- | ---: | ---: | ---: | ---: |
| signal `deserializeN(200)` | 35,379.10 ns | 35,057.65 ns | **0.9908x** | 15/15 |
| signal `serializeN(200)` | 18,857.79 ns | 18,819.82 ns | **0.9980x** | 12/15 |
| explicit `deserializeN(200)` | 40,313.52 ns | 40,241.12 ns | **0.9985x** | 11/15 |
| explicit `serializeN(200)` | 19,971.61 ns | 20,033.45 ns | 1.0030x | 2/15 |

The explicit serializer movement is 0.3% and below the retention threshold;
the signal-bounds mode used for the performance goal improves both exports.
Total generated native code for the module fell from 73,504 to 73,424 bytes.
The retained differential test covers serial and parallel compilation plus
cold and warm artifact-cache paths against Railshot. Broader callee-preserved
sets, forced RailMach admission, accessor inlining, and a direct comparison-to-
branch fold were rejected after correctness failures or neutral/regressing
paired measurements.

The next retained slice fuses recognized UTF-16 whitespace loops. Since their
character is proven to come from `i32.load16_u`, `(character - 9) & 65535 <= 4`
is exactly equivalent to the unsigned comparison without the mask. Direct
branches for that range and the separate space character replace two
materialized booleans, their OR, and the final boolean branch. On signal bounds,
15 alternating 500 ms pairs measured `json-as-simd.deserializeN(200)` at
34,466.30 ns versus 34,815.07 ns before the change: **0.9896x**, with 15/15
wins. Scalar `json-as.deserializeN(200)` was neutral at 0.9992x. The SIMD
module's native code fell another 288 bytes, from 73,424 to 73,136 bytes; its
array parser fell from 2,184 to 2,144 bytes. Differential coverage injects each
accepted UTF-16 whitespace value (9 through 13 and 32) into the encoded corpus
input. Small-fill tail decomposition and extra parser-local pinning were
rejected after their signal-bounds gains remained below the retention threshold.

The following slice folds the parser's immediate post-loop
`pointer >= end; br_if` into that same whitespace scan. Exhaustion branches
straight to the original enclosing label, while observing a non-whitespace
character proves the repeated guard false. This removes 40 bytes from the array
parser and 96 bytes from the signal-bounds module. Fifteen alternating 500 ms
explicit-bounds pairs measured `deserializeN` at a **0.9948x** geometric-mean
ratio with 9/15 wins. A shorter six-pair signal-bounds run was noisy and measured
1.0064x, so the retained claim is the instruction/code-size reduction and the
explicit paired result, not a signal-bounds latency win.

A subsequent register-only decimal tail-loop experiment removed another 144
module bytes, but 15 alternating 500 ms signal-bounds pairs were neutral at
0.9993x with 8/15 wins, so the compiler specialization was rejected. The guard-
page JSON serialize/deserialize benchmarks added for that experiment remain as
the repeatable retention gate for future parser work.

Windows ARM64 currently uses the structured emitter for every function. Native
CI showed that small direct-entry fixtures passed while mixed structured and
RailMach JSON modules corrupted linear-memory state and the guard-page handler
later faulted. The earlier call/global-only fence was insufficient because it
left RailMach leaves behind that boundary. Until the Windows register/unwind
boundary has a native proof, the platform therefore keeps the ordinary verified
wrapper and structured internal convention. Linux and Darwin retain RailMach's
measured register-entry and widened private-call paths.

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
