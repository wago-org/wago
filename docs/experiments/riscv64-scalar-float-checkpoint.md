# RV64 scalar floating-point execution checkpoint

The production compiler and opt-in `wago_rv64_experimental` public backend now
lower F32/F64 arithmetic, comparisons, rounding, sign operations, precision and
integer conversions, reinterpretation, and all eight trunc_sat instructions.
Scalar FP globals use literal bit initializers and the existing aliased cell
directory. FP loads/stores use the same checked bytewise memory32 lowering as
integers, preserving raw bits even at unaligned addresses. Unsupported module
shapes still reject before publication. The isolated GuestContext remains an
integer-only diagnostic ABI.

Frames, locals, joins, branch vectors, same-instance calls and return slots carry
raw bits in canonical GP/stack homes. F32 carriers clear their upper word, and
F64 carriers retain all bits. Only caller-saved f0..f2 are temporary; they do not
span calls or cooperative polls. Register-ABI/prepared proofs remain absent.
The reviewed shared bridge preserves complete FCSR on normal and cold return.

Arithmetic/conversion instructions select static rounding modes. Baseline F/D
rounding-to-integral lowers through an integer below magnitude 2^63; larger finite
FP values are already integral. Sign injection retains negative zero. RISC-V's
single-NaN min/max behavior is corrected by checking both operand classes first.
Arithmetic NaNs are canonical, while sign/reinterpretation/transport retain raw
payloads. Trapping conversions reject NaNs and conversion NV with production
TrapTruncOverflow and exact source PC. Clearing conversion flags retains FRM and
prevents prior guest arithmetic from poisoning valid truncation. Saturation maps
NaNs to zero and otherwise uses the ISA's signed/unsigned clipping semantics.

The public gate admits only I32/I64/F32/F64 signatures and literal globals within
the existing implementation bounds. The feature catalog adds nontrapping float
conversions. Trusted architecture-3 artifacts retain this framed, polling ABI;
function/global imports, tables/references, host callbacks, SIMD, GC/EH and unsupported resource
shapes remain gated. No other architecture supplies fallback machine code.

Independent numerical and runtime/state reviewers accepted the lowering and
public gate. Host/core QEMU tests passed three runs in ordinary and checked modes.
Numerical matrices cover every scalar operation, infinities, subnormals, boundary
integers, signaling/quiet NaNs, signed zero and dirty F32 carriers. Transport
fixtures cover 256 mixed arguments/results with zero warm allocations, live raw
payloads across FP calls, indexed control and actual loop backedges, selects,
global-cell sentinels, unaligned memory and no effects on failed accesses.

Runtime probes preserve seeded RUP/flags on normal, cold and rejected entry, and
independently distinguish static RNE from dynamic rounding with a half-ULP tie.
An emitted-code scanner rejects dynamic/reserved rounding fields. Public fixtures
cover artifact round trips and instance ownership after Compiled.Close, literal
globals, raw state, ordinary calls/WasmFunc/sessions, warm zero allocation,
conversion traps/reuse, saturation and disabled-feature rejection. Loaded-image
FP-loop cancellation waits for a real FP update and foreign-call completion
before cleanup and verifies later FP reuse. Portable public fixtures also run on
the AMD64 reference backend. Qualification logs are `evidence/float-*.log` in the
task workspace.

This is bounded scalar correctness, not full production parity. QEMU does not
qualify native cache behavior or performance. One ordinary memory32 import is now covered by the
[imported-memory checkpoint](riscv64-imported-memory-checkpoint.md). Other imported
resources, host calls,
table/reference dispatch, SIMD, GC/EH, threads and broader build/feature suites
remain in [the parity tracker](riscv64-parity-tracker.md).
