# wago roadmap

wago is a pure-Go (no cgo) single-pass WebAssembly engine — a from-scratch port
of [WARP](https://github.com/wago-org/warp)'s design. Linux, macOS, and Windows
on amd64 and arm64 are supported. The amd64 backend uses a modern CPU baseline
of SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI remain
outside the baseline and require explicit feature gates. This file tracks what
works and what's next at a glance.

Four companion docs go deeper:
- [FEATURES.md](FEATURES.md) — the per-feature support matrix (source of truth for
  spec-feature status).
- [OPTIMIZATIONS.md](OPTIMIZATIONS.md) — the optimization roadmap (what codegen work
  is landed / pending, and why).
- [docs/no-ir-plan.md](docs/no-ir-plan.md) — the phased execution plan (P0–P8) that
  the "Next" section below is a summary of.
- [docs/wasm3.md](docs/wasm3.md) — the mandatory Core 3.0 implementation ledger,
  official suite pin, measurements, platform gates, and recursive slices.

Status: [x] done · 🚧 in progress · [ ] planned.

## Done

**Full WebAssembly 1.0 (MVP).** The pinned pre-reference-types spec testsuite passes
in full — 57/57 applicable files, 0 failing assertions (see [SPECTEST.md](SPECTEST.md)).

**Frontend (`src/core/compiler/wasm`)**
- [x] Binary decoder for all sections; byte-backed `DecodeModule` (function bodies
  stay raw bytes, not materialized AST)
- [x] Full validator (operand/control stack typing), byte-backed and differential-tested
  against the official spec testsuite; independent function bodies support bounded,
  deterministic parallel validation through the function-worker policy

**Compiler backend (`src/core/compiler/backend/railshot`)**
- [x] Single-pass x86-64 codegen with the WARP Valent-Block register allocator
  (symbolic operand stack, deferred-action trees, whole-register-file allocation,
  spill-to-canonical-slot)
- [x] Value types **i32, i64, f32, f64** — arithmetic, bitwise, shifts/rotates,
  clz/ctz/popcnt, comparisons, conversions, reinterpret, `ceil`/`floor`/`trunc`/
  `nearest`/`copysign`, trapping float→int truncation, `trunc_sat`, sign-extension ops
- [x] Control flow: block / loop / if / else / br / br_if / br_table / return
- [x] Linear memory load/store (all widths, signed/unsigned); two bounds modes —
  explicit (memBytes in R15) and guard-page (`-tags wago_guardpage`)
- [x] `memory.size` / `memory.grow` (up-front reservation, grow to declared max)
- [x] Bulk memory `memory.copy` / `memory.fill` (small-n unrolled; forward `rep movsb`) plus passive data `memory.init` / `data.drop`
- [x] Calls: direct, recursion, `call_indirect` (table + signature check) over a
  single-result **register ABI** with a parallel-move resolver; host imports
  (numeric scalar and `v128` params/results via synchronous re-entry, legacy void
  `HostFunc` replay, host functions usable as table funcrefs)
- [x] `select` / `select t`; active element and data segment initialization; `start`
- [x] Hotness-aware local pinning + value-pinned/module-pinned hot globals
- [x] Bounded parallel function codegen with worker-local scratch/arenas and
  deterministic ordered assembly, sharing one policy with function validation

**Runtime (`src/core/runtime`)**
- [x] No-cgo execution: W^X `mmap`, foreign-stack trampoline, `g` preservation,
  trap→error, zero-copy linear memory
- [x] Cross-instance linking: function / global / table / memory imports & exports,
  including shared mutable tables + memories. Imported calls compile once and bind
  through per-instance dispatch cells with explicit direct/indirect context switching.
- [x] Instance slot reuse (lower instantiate cost — explicit #105, guard-page #108)

**Tooling**
- [x] `wago` CLI: `run` / `validate` / `version`, typed args, and explicit `--core 3` opt-in while preserving the Release 2 default
- [x] Public API: `Run`/`RunValues`, `Compile`/`Compiled`, `Instance`, plus
  opt-in serial/adaptive/forced function-worker policy for validation and codegen
- [x] Workers plugin: the separate `github.com/wago-org/workers` extension
  owns a transactional worker service with bounded copied tagged delivery,
  cooperative kill, neutral exit events, and creator-authorized lifetime links;
  actor/PID/mailbox/supervisor policy remains plugin-owned
- [x] `wago run` and `wago validate` expose adaptive/forced function workers via
  `-p`, with serial defaults for predictable memory use
- [x] Benchmarks vs wazero (compile ~34× faster; wago wins fib_rec, sieve, memory_tree,
  linked_list, dispatch, branches, json deserialize; loses on json serialize, blake)

**Arm64 acceptance (in progress)**
- [x] Parent/child corpus runner with hard per-case deadlines and explicit/guard/wazero outcomes
- [x] Darwin/arm64 guard-page execution via synchronous SIGSEGV/SIGBUS context rewriting (Mach-port receiver avoided)
- [x] Guard-page execution on all six native targets, including Darwin/amd64
  signal-context rewriting and Windows vectored exception handling
- [x] Verify json-as serialize/deserialize in explicit and guard modes and SQLite's
  recursive-CTE aggregate workload against committed goldens on Darwin/arm64
- [x] Reference globals, heterogeneous indexed table operations, and nonzero-table
  `call_indirect`, with native Linux/arm64 and Darwin/arm64 CI gates
- [x] Explicit-bounds `CoreFeaturesV3`, including tails, typed references, WasmGC,
  exception handling, multi-memory, memory64, and table64, with a zero-gap complete
  official suite under Linux/arm64 QEMU and native Linux/Darwin arm64 CI gates

## Next (near-term)

The optimization plan remains **[docs/no-ir-plan.md](docs/no-ir-plan.md)** and the
Core 3.0 plan is **[docs/wasm3.md](docs/wasm3.md)**. Current tracks:

**WebAssembly 3.0** (primary conformance complete; product hardening continues)
- [x] Pin and execute the complete official `WebAssembly/spec` `wg-3.0` corpus.
  Linux/amd64 native and Linux/arm64 QEMU explicit-bounds `CoreFeaturesV3` runs
  pass all 2,226 modules and 58,038 assertions with zero failures, skips, or gap
  categories; native Linux/Darwin arm64 runs are now required in CI.
- [x] Complete mandatory extended constants, relaxed SIMD, tails, typed function
  references, GC, exception handling, multi-memory, memory64, and table64 on the
  primary product. Release 1/2 defaults remain unchanged; Core 3 is opt-in.
- [x] Add exact linux/amd64 WasmGC roots across local direct/indirect/reference
  calls, recursion, bounded host re-entry, mutable/shared GC globals, local/shared
  collector-reference tables, EH payload records, and same-Runtime cross-instance
  calls. Codec v30 persists and validates the native root metadata.
- [x] Add snapshot v4 stable-ID heap graphs for objects reachable from owned local
  GC globals, preserving cycles and sharing without serializing compact handles.
- [x] Add snapshot v5 roots for one owned local collector-reference table, then
  snapshot v6 roots for multiple heterogeneous local tables, including persisted
  growth state, cross-table cycles/sharing, strict structural subtype validation,
  malformed-input rejection, and native ARM64 execution.
- [x] Lower struct, array, i31, cast/test, and conversion helpers on linux/darwin
  arm64 through the synchronous parked-host ABI.
- 🚧 **Iteration 82 — hardening the new ownership and persistence boundaries:** add
  independent-domain, rollback, multi-hop, codec, close-order, moving-root, mixed
  graph, malformed snapshot, fuzz, and native-arm64 execution coverage.
- [x] **Arm64 Core 3 explicit-bounds parity:** exact local/hidden-spill maps walk
  direct and recursive calls, suspended direct-host activations, same-domain
  foreign frames, indirect/reference calls, discarded-frame proper tails, and
  fixed EH payload roots. Dynamic indirect tails, function-subtype tests/casts,
  indexed memory operations, `try_table`, `throw`, and `throw_ref` are admitted.
  Warmed recursive Throughput/Tiny collection remains allocation-free by using a
  direct immutable-root visitor. Polymorphic local `call_indirect` and same-domain
  foreign `call_ref` now publish every internal/wrapper/cross-instance return path,
  including the wrapper save-area stack adjustment. Exact imported `ref.func`
  provenance performs a discarded-frame cross-instance `return_call_ref` through
  the bounded tail transfer. Polymorphic foreign tails and other shapes with
  unproved ownership remain fail-closed.
- [x] **Shared GC globals:** imported/exported mutable and immutable collector-reference
  globals share canonical structurally compatible Runtime domains. Collection scans every live alias
  cell directly, checked slots preserve barrier/card state, and rollback, codec,
  close-order, Throughput/Tiny, amd64, and ARM64 execution are covered.
- [x] **Shared GC tables:** multiple heterogeneous imported/exported
  collector-reference tables share direct alias roots, growth state, indexed native
  roots, attachment rollback, codec reload, and producer-first close across
  canonical structurally compatible Runtime domains. Incompatible domains remain fail-closed.
- [x] **Cross-instance persistent GC graphs:** exact GC-reference function imports
  coexist with shared GC globals and heterogeneous tables under Throughput/Tiny
  collection, codec reload, attachment rollback, and producer-first close.
- [x] **Snapshot v5/v6 local-root hardening:** v5 preserves one owned collector
  table; v6 preserves multiple heterogeneous local tables with indexed lengths,
  cross-table sharing, deterministic repeated capture, strict subtype validation,
  and near-capacity restore rollback on amd64 and Linux/ARM64.
- [x] **Cross-module canonical GC type identity:** Runtime domains canonicalize
  recursive structural types and give every instance an immutable local/domain type
  map. Structurally equivalent producer/consumer modules with reordered or additional
  local types share one collector without treating raw module-local indexes as identity;
  collector descriptors append safely under the domain lock, codec-loaded modules and
  checked host tokens use the same mapping, and incompatible layouts/configurations
  remain fail-closed.
- [x] **Whole-domain snapshots:** `CaptureDomain` quiesces and exhaustively captures an
  explicitly ordered Runtime collector domain: every member module/memory/global/table,
  internal function/global/table import edge, alias identity, and one shared stable-ID GC
  graph. `DomainSnapshot.Instantiate` restores acyclic internal links and publishes the
  member slice only after graph reconstruction; any failure closes the unpublished domain.
  The `WGDN` v1 blob persists compiled members, exact GC configuration, imports, aliases,
  cycles, sharing, and heterogeneous canonical types; restore requires a Runtime without
  an existing live GC domain. Live public GC tokens, active calls,
  external imports, imported/shared memories, opaque references, passive elements,
  imported exception tags, incomplete member sets, and cyclic instantiation graphs
  reject before publication. Local exception-tag directories and completed EH state
  need no extra mutable snapshot payload and restore through the compiled member.
- [x] **Bounded host-held GC results:** generic struct/array results issue up to 64
  opaque `GCRef` tokens per producer, atomically roll back partial multi-result egress,
  reuse released checked slots, retain exact Runtime/store ownership after producer
  close, and reject stale/cross-producer release.
- [x] **Host GC token ingress:** non-null `GCRef` arguments re-enter only the exact
  collector domain after structural subtype validation, use up to 64 reusable checked
  roots, survive concurrent release after staging, and reject stale/foreign tokens.
- [x] **Linux/amd64 bounds-mode parity:** explicit and signal-backed Core 3 both
  pass 2,226 modules and 58,038 assertions with zero failures, skips, or gaps.
  Signal mode keeps explicit directory checks for nonzero memories and full-u64
  checks for memory64 while using guard-backed owned mappings. Native Linux/Darwin
  arm64 explicit-bounds runs remain mandatory CI cells; broader arm64 bounds-mode
  qualification continues separately.
- [ ] **GC hot paths:** after the correctness matrix is complete, add measured
  direct checked JIT object access while retaining helper slow paths.

**Engine & performance** (no-ir-plan P1–P7, measured against P1's stats)
<!-- roadmap:P1 status=done -->
- [x] **P1 — `CodegenStats` + explain mode**: per-function counters,
  `WAGO_EXPLAIN`, golden-disassembly harness, `WAGO_DEBUG_MODGLOBALS`, and
  `WAGO_PIN_GLOBAL_K` are implemented on amd64 and arm64.
<!-- roadmap:P2 status=partial -->
- 🚧 **P2 — cheap railshot wins**: the const-fold pack and same-operand integer
  identities are landed; alias-aware pending loads, pure-tree `drop`, and
  narrow-load mask elision remain measurement-gated.
<!-- roadmap:P3 status=partial -->
- 🚧 **P3 — `stFlags` and compare fusion**: eqz-of-compare inversion and ordered
  float compare-to-branch fusion are landed; broader flags-resident consumers
  remain measurement-gated.
<!-- roadmap:P5 status=partial -->
- 🚧 **P5 — calls**: ARM64 mixed GP/FP parallel staging, two-integer-result
  `X0/X1` returns, and proven monomorphic indirect calls are landed. Broader
  multi-result register shapes and mutable-table epoch caches remain.
<!-- roadmap:P6 status=partial -->
- 🚧 **P6 — memory & bounds** (explicit mode): straight-line bounds facts are
  implemented; hybrid loop prechecks, store combining, load-after-store
  forwarding, and a CPUID-gated BMI2 path remain.
<!-- roadmap:P4 status=planned -->
- [ ] **P4 — restricted pending `local.set`/`tee`** *(gated on P1 counters)*
<!-- roadmap:P7 status=planned -->
- [ ] **P7 — compile path** *(premise re-measured post-#96)*: fused validate+compile

**Runtime & product** (no-ir-plan P8 — parallel track, feature value)
- [x] **Synchronous host-import results** — returning host imports use the no-cgo
  re-entry protocol; `v128` host params/results use the same two-slot public ABI.
- [x] Interruption / cancellation: Linux/amd64 and Linux/arm64 use
  thread-directed real-time signals plus validated `ucontext` rewriting, with
  zero interruption instrumentation in generated Wasm. Other targets retain
  function-entry and loop-header polls. Both Linux architectures return
  `context.Canceled`/`DeadlineExceeded`, and active-instance close uses the same
  trap. See `docs/linux-host-interrupt.md`.
- [x] Wasm-level trap source frames: generated cold edges report the logical
  function (including inlined callees) and an exact Wasm PC when a function has
  one site for that trap class. Shared multi-site stubs still report the
  function without guessing a PC. Full caller-chain unwind metadata remains a
  follow-up.
- [x] WebAssembly 2.0 product closeout: `.wago` codec v27 persists structural
  reference globals, indexed typed tables/exports/elements, exact local/imported
  table/memory-limit forms, indexed memory imports/exports, and required-feature
  bits without serializing live runtime
  identity. Snapshot products reject every table/reference-global module.
  Deterministic module inspection reports all
  reference signatures/globals and every table/import/export/index/type/limit,
  including duplicate aliases and loaded modules. Consolidated trap and cross-link
  teardown tests cover globals, multiple table aliases, passive elements, store
  bindings, and producer/consumer close order. The official Release 2 execution
  harness remains zero-skip at 1,600 modules / 48,248 assertions.
- [ ] `call_indirect` inline caches behind a table epoch
- [x] `.wago` productization: `wago build` creates explicit artifacts and
  `wago run` reuses an automatic cache keyed by the module, exact runtime
  executable, GOOS/GOARCH, effective feature/bounds/memory configuration, and
  optimization knobs. `wago cache` owns inspection, pruning, and cleanup.

## Verification & quality

- [ ] Differential oracle: fuzz modules, compare results/traps against C++ WARP (the
  off-path `src/core/compiler/ir` package is reserved as this oracle)
- [ ] Byte-for-byte codegen diffing against WARP for shared inputs
- [ ] Golden disassembly regression net (grows one golden per optimization from P1 on)

## Bigger bets

- [x] SIMD (`v128`) — complete for the documented linux/amd64 SSSE3/SSE4.1 + AVX/VEX.128 baseline: every decoded core SIMD opcode and deterministic relaxed SIMD opcode through 0xfd 275 is frontend-admitted, validator-admitted, and lowered by railshot; reserved proposal-table holes are invalid-decode tests. Public `[16]byte` (`wago.V128`) plumbing covers locals, params/results, control flow, globals, cross-instance imports, and host imports/results. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). Keep AVX2/FMA/VNNI optimizations behind future CPU gates. Current metrics: [`docs/simd-performance-2026-07.md`](docs/simd-performance-2026-07.md).
- [ ] Threads & atomics — tabled until the remaining WasmGC cross-module ownership
  and whole-domain snapshot correctness work is complete; actor/mailbox reference
  transport remains plugin/product work rather than part of the Core 3 closeout.
- [x] Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`) —
  complete for the Core 3 linux/amd64 explicit-bounds product, including local,
  host, cross-instance, indirect, typed-reference, trap, and validation paths.
  Broader platform and bounds-mode parity is tracked above.
- [x] Basic extended constant expressions: integer add/sub/mul, prior immutable
  globals, active offsets, strict validation, and codec-v29 persistence.
- [x] Typed function references — recursive structural typing, typed tables,
  elements and globals, `call_ref`, casts/tests, null branches, linking, ownership,
  codec metadata, and official invalid/unlinkable behavior are complete for the
  primary Core 3 product.
- [x] Multi-memory, memory64, and table64 — mandatory Core 3 execution, metadata,
  codec, linking, traps, and official tests are complete on linux/amd64 explicit
  and signal-backed bounds. Linux/Darwin arm64 explicit-bounds execution is
  implemented; the full official suite passes under Linux/arm64 QEMU and native
  Linux/Darwin arm64 runs are required in CI. Broader arm64 guard-page parity
  remains a platform-qualification item.
- [x] Reference-types product completion: signatures, locals, control,
  local/imported/shared globals, host ABI, explicit host funcref ownership/egress,
  typed 8-byte externref tables/elements, every `table.*` operation, multiple
  local/imported tables, exact exports/re-exports, codec-v27 structural metadata,
  snapshot isolation, complete inspection, cross-link teardown, and the
  zero-skip Release 2 execution corpus are done.
- [x] Native Linux, macOS, and Windows runtime paths on amd64 and arm64, with
  mandatory native CI and release assets for all six targets.
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals (for now)

- An interpreter tier (wago is single-pass JIT only)
- **An SSA / IR execution tier** — decided against 2026-07-03; railshot is the one and
  only backend, and the ceiling is attacked incrementally instead
  (see [docs/no-ir-plan.md](docs/no-ir-plan.md) §0)
- Re-implementing WARP's linker/disassembler/fuzzer (WARP remains the external
  reference)

### Iteration 72 boundary

M8 source lines 652–659 are now admitted as a separate exact product: two duplicate recursive function groups, two provider exports, and four ordered consumer import views. Provider/consumer wasm/code/codec sizes are 100/253/531 and 92/0/315 bytes; descriptor arenas are 96/160 bytes; duplicate imports retain one producer; both instances remain collector-free. Accounting is 40 modules / 23 assertions / 5 gates / 11 blocked commands / 24 invalid / 6 executed plus 2 blocked unlinkables. Source line 668 and later products remain fail-closed.

### Iteration 73 boundary

Official `gc/type-subtyping.wast` is complete: 45 modules, 29 assertions, 24 invalid modules, and 8 expected unlinkables all execute or reject exactly, with zero gates or hidden failures. M9 uses bounded 96/288-byte provider/consumer descriptor arenas and one retained producer across eight imports; M10/M11 reject before retention; the six non-flat f32 exports execute collector-free.

## Iteration 74 Core 3 completion

The remaining feature families are integrated under explicit `CoreFeaturesV3`
admission. The pinned official Release 3 suite completes at 2,226 passing modules
and 58,038 passing assertions with zero failures, skips, or gap categories. The
final integration includes prior-local-global constant offsets, typed element
initializers, generic `array.new_data`/`array.new_elem`, imported/exported tags,
`spectest.table64`, shared-memory co-tenant serialization, and reference
argument/result ownership. Release 1/2 defaults remain unchanged; Core 3 is an
explicit opt-in outside the versioned spec harness.

## Iteration 75 generated WasmGC smoke hardening

A real MoonBit Starshine CLI artifact now compiles, links, and completes its start
function under `CoreFeaturesV3` plus explicit bounds. The 3,225,249-byte payload
has SHA-256 `3a92309ca48f80594c88ea6c3508982d6fc34953c018ce31786382e08a18d046`.
Admission is derived from validated struct/array opcodes instead of export names
or exact binary hashes; multi-field/reference constructors, reference stores,
indexed `ref.null`, object-building constant expressions, declared-subtype
struct/array access, and opaque 64-bit function/extern fields are supported.

The amd64 synchronous helper path now homes and restores caller-saved pinned
locals, and both native control frames carry 64 slots. General generated WasmGC
execution remains sound without native frame maps by forcing a bounded
collection-disabled Throughput heap; exhaustion fails explicitly instead of
collecting from an incomplete root set. Constructor operands are rooted
atomically, and one mutex-protected 63-value instance scratch removes per-helper
Go allocations.

Measured on the Ryzen 7 8845HS host, Starshine compile improved from about
1.30 s / 1.85 GB allocated to roughly 1.03 s / 74.8 MB after reusing one
flattened type converter. Compile+link improved from about 2.02 s / 2.38 GB to
1.52 s / 241 MB after additionally bounding dense per-function global-hint
scoring. A fresh isolated cold link/JIT is 0.602 s / 166.2 MB; linked
instantiation/start is 31.7 ms / 3.13 MB. The synthetic subtype
`struct.new`/`struct.set`/`struct.get` path measures 383-416 ns/op, 0 allocs/op.

Next work is exact native safepoint publication and root updates across arbitrary
calls/traps, followed by mutable GC global/table synchronization, public and
cross-instance object ownership, persistence/snapshot semantics, guard-page GC,
and non-amd64 native lowering. The Starshine smoke does not convert those items
into a general-purpose WasmGC claim.

## Iteration 76 deterministic MoonBit JSON smoke

A checked-in source fixture under `testdata/moonbit-json-smoke` now supplies a
small, reproducible semantic gate alongside the large Starshine startup test.
MoonBit 0.1.20260703 builds it into a 44,023-byte import-free WasmGC module with
SHA-256 `b4e33e0685aa5572516ab037be12a3ad1aee93ab9891ba4071c42c23a3e9ca2d`.
The exported `run(i32) -> i64` parses, stringifies, reparses, compares, and
checksums a nested JSON corpus; pinned results cover 1, 2, and 8 iterations.
`make test-moonbit-json` checks the exact compiler version, rebuilds the module,
verifies its canonical bytes, and executes it through Wago.

The fixture found a shape-independent compiler bug before execution: dead-code
lowering did not consume `0xfb` GC instruction immediates, so an unreachable
`struct.new` desynchronized the remaining function body. Both amd64 and arm64
now use the canonical bytecode classifier for GC-prefix immediates, with direct
regression tests. Starshine remains the scale/startup benchmark; the JSON module
is the deterministic execution gate. Its pinned production compile baseline is
10.641 ms, with 0.276 ms decode, 1.380 ms validation, 0.170 ms instantiate, and
4.733 ms for fresh instantiate plus one checked JSON run.

## Iteration 77 first native WasmGC frame roots

The first linux/amd64 native-root slice enabled collection inside one generated
WasmGC invocation by publishing typed local qwords through parked RSP. Its
branch-and-loop regression preserves a `struct<v128>` local across 1,000
allocations under Throughput forced-major verification and Tiny incremental
stress while keeping the warmed helper path allocation-free.

## Iteration 78 liveness-exact safepoints and recursive frame walking

The single-function product now computes exact local liveness over the validated
structured Wasm CFG, assigns one compact ID per reachable allocation, and adds
live hidden operand spills after canonical stack flushing. Dead locals disappear
from site maps, hidden references survive control merges, and actual off-heap
qwords remain mutable collector slots.

Direct numeric local calls record native return PCs, caller frame sizes, and the
roots live at each callsite. The runtime walks cross-function and recursive
frames from parked RSP until a validated adapter return, preserving caller
objects while the deepest frame performs 1,000 allocations under Throughput and
Tiny stress. Direct tail calls discard each caller frame and retain no callsite roots. Codec v30
persists and strictly validates frame sizes, safepoint ordering, root alignment,
callsite returns, and adapter termination. Forged metadata fails closed. Five
500 ms samples measured 432.5-443.5 ns/op, 0 B/op, and 0 allocs/op. The expanded
direct walker raises lazy `gcPublicState` from 1,560 to 2,440 bytes while the
64-byte `compiledCodeCache` layout remains unchanged.

## Iteration 79 suspended host activations and mutable globals

Numeric host imports now record dynamic-wrapper stack adjustments and preserve
up to eight suspended activations. Each nested callback borrows a separate
foreign execution stack, while the outer control header and exact native roots
remain parked. Boundary and allocating-helper collections scan every suspended
activation. Codec v30 persists and validates the new callsite shape. Throughput
forced-major and Tiny collect-every-allocation stress preserve an outer struct
across 1,000 allocations in a re-entered function, including codec reload.

Generic module-local GC globals now synchronize their checked collector slots
before every allocating helper as well as invocation-boundary collection, so a
mutable global may retain an object throughout a long-running invocation.
`gcPublicState` is 2,440 bytes on amd64; the state remains lazy and warmed helper
publication remains allocation-free.

## Iteration 80 indirect calls, table roots, and guard execution

Private immutable local-function tables now admit `call_indirect` with exact
caller callsite maps and `return_call_indirect` with discarded caller frames.
Imported/exported/mutable function tables remain outside the proof. One private
mutable collector-reference table is scanned directly from its validated
off-heap descriptor at invocation boundaries and allocating helper safepoints;
collector rewrites update the actual table qwords. Tiny stress preserves table
objects across 1,000 allocations and codec reload.

Generic struct/array helpers and exact native frame roots now execute with
linux/amd64 guard-page bounds checks. A tagged test performs 1,000 Tiny
collect/step-every-allocation iterations and verifies a live `v128` object.
`gcPublicState` is 2,440 bytes on amd64.

## Iteration 81 local call-ref and EH root unioning

`call_ref` and `return_call_ref` now enter the collection-enabled subset when all
function descriptors are proven same-module: no function-reference parameters,
results, globals, imports, or mutable/exported function tables may feed the
site. Non-tail calls persist the internal return PC; tail-ref calls discard the
caller frame.

EH functions use conservative all-reference-local masks at allocation/call
sites and merge the backend's fixed GC-payload record offsets into each map.
The native EH lowering zero-initializes records before use, copies caught
payloads into them, and clears dropped records. Numeric `try_table` Tiny stress
preserves an outer object across 1,000 allocations, including codec reload.

Iteration 82 completed the first bounded forms of all three: exact same-Runtime
collector domains for descriptor-identical global/table-free modules, arm64 helper
lowering, and snapshot-v4 stable-ID heap graphs rooted by owned local GC globals.
Arm64 now collects through direct/recursive calls, suspended direct-host
activations, same-domain foreign frames, subtype-checked indirect/reference calls,
proper discarded-frame tails, and fixed EH payload records, with mutable/shared GC
globals and local/shared collector tables. Bounded `try_table`, `throw`, `throw_ref`,
indexed multi-memory, memory64, and table64 execution complete the explicit-bounds
Core 3 product. Polymorphic or foreign reference calls remain collection-disabled
where exact ownership is unproved. Next work broadens shared GC globals/tables and
snapshot roots, then completes signal-backed and broader native-platform parity.
