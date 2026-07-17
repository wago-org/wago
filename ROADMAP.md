# wago roadmap

wago is a pure-Go (no cgo) single-pass WebAssembly engine — a from-scratch port
of [WARP](warp/)'s design. Target today is **linux/amd64** with a modern CPU
baseline of SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI remain
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
- [x] `wago` CLI: `run` / `validate` / `version`, typed args
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
- [x] Verify json-as serialize/deserialize in explicit and guard modes and SQLite's
  recursive-CTE aggregate workload against committed goldens on Darwin/arm64
- [x] Reference globals, heterogeneous indexed table operations, and nonzero-table
  `call_indirect`, with native Linux/arm64 and Darwin/arm64 CI gates

## Next (near-term, linux/amd64)

The optimization plan remains **[docs/no-ir-plan.md](docs/no-ir-plan.md)** and the
Core 3.0 plan is **[docs/wasm3.md](docs/wasm3.md)**. Current tracks:

**WebAssembly 3.0** (bounded recursive slices; linux/amd64 is the primary claim)
- [x] Pin official `WebAssembly/spec` `wg-3.0` at `9d360199...`; route `make spec3`
  to its 258-file `test/core` corpus and make parser failures/skips fail closed.
- [x] Add `CoreFeaturesV3`, separate admission bits for mandatory families, and
  explicit `GOOS/GOARCH` unsupported-feature errors.
- [x] Execute the basic extended-constant-expression proposal and persist deferred
  scalar initializers/offsets in `.wago` codec v27 (initializer records introduced in codec v21).
- [x] Bootstrap checksum-pinned WABT 1.0.41, then pin the official
  WebAssembly/spec 3.0.0 interpreter at the suite revision for the 28 files WABT
  cannot parse. The schema-2 258-file inventory now has zero parser failures,
  144 green/114 red files, 535 skipped modules, 5 failed and 6,268 skipped
  assertions; tool/parser failures remain hard.
- [x] Honor official Release 3 relaxed-SIMD `either` result patterns: all 8
  converted modules and 69 assertions pass with zero failures/skips.
- 🚧 Tail calls and typed references: amd64 local direct, private-immutable-table
  mixed GP/XMM indirect, descriptor `call_ref`, and same-instance int-register
  `return_call_ref` milestones execute internally with bounded frames and trap
  checks. `ref.func` now preserves its indexed type, recursive structural type
  equivalence validates, and a staged frontend gate routes indexed signatures to
  `call_ref`. Public structural type descriptors, exact signature/global/table/
  element inspection, and codec-v27 persistence are now present. Staged runtime
  storage/import matching uses cross-module structural subtype/equivalence;
  native descriptors use bounded 64-bit structural keys as fast discriminators.
  A bounded runtime/reference-store registry resolves every equal key against the
  complete cross-module structural descriptors before publishing native targets,
  rejecting distinct collisions transactionally. Recursive-group canonicalization
  is module-cached and fails closed above a fixed expansion budget; public invocation,
  synchronous host boundaries, and mutable global ingress/egress enforce exact
  types/nullability. Dynamic typed `table.get/set/grow/fill/copy/init` now prove
  shifted-type imports/re-exports, producer replacement, close order, and trap
  atomicity; local table-owner overwrites release closed consumers. amd64 executes
  `ref.as_non_null` and both null branches; local wrapper direct tails use a fixed
  16-slot bank; and the reached `select` funcref assertion is green. Cross-instance
  `InstanceExport` imports now retain each distinct producer through consumer close,
  so shifted typed `call_ref` descriptors remain valid after producer logical close.
  Typed/tail opcodes persist required-feature bits and snapshots reject unresolved
  descriptor/tail contexts before mutation. A compile-only typed-tail gate now lets
  retained int-register `InstanceExport` descriptors transfer through a foreign
  wrapper on amd64 from root or nested internal callers. Nested callers reuse one
  fixed 32-byte return record, restore both integer results through a trampoline,
  and repeat 10,000 cross-instance transfers without retaining the discarded callee
  frame. Exact typed globals may tail-enter tagged same-instance scalar wrappers;
  hosts remain untagged and fail closed. A private direct-tail gate plus the existing
  host bridge now makes all 47 pinned `return_call` commands green: 3 modules, 33
  assertions, and 11 invalid modules. Per-table finite immutable-local proofs plus
  staged scalar descriptors and fixed-bank wrapper tails now make all 79
  `return_call_indirect` commands green: 3 modules, 49 assertions, 16 invalid, and
  11 malformed. The pinned `return_call_ref` runner is now gap-free at 51 commands,
  5 modules, 35 assertions, and 11 invalid modules; one canonical funcref result uses
  the staged RAX return path with exact descriptor ownership. Retained cross-instance
  direct `return_call` uses a separate fixed four-word root/nested transition, preserves
  producer lifetime and trap recovery, repeats without allocation, and now admits exactly
  `(i32, f64) -> f64` and `(f64) -> i32` in addition to the integer shapes. A complete
  14-file typed-reference/structural matrix is now gap-free at 422 commands with 61 modules,
  246 assertions, all 65 invalid modules, 2 malformed, and 2 unlinkable cases passing.
  `call_ref`, null control, both null-only `ref_null` modules, and every valid `type-rec`
  product execute under staged admission. The ten former struct-defined leaders are exact
  collector-free metadata/function-identity products: immutable local `ref.func` globals,
  cross-instance link matching, and ordinary funcref `call_indirect`. Whole non-singleton
  recursive groups now contribute group order and selected-member position to the bounded
  64-bit discriminator, while exact store admission prevents hash equality from being the
  sole native type authority. No struct/array value is allocated or accessed. Public admission, other float/
  oversized direct tails, general-table/
  foreign-float/general reference-result tails, live typed snapshot state, remaining
  GC/reference instructions, and arm64 parity remain gated.
- 🚧 Multi-memory now has an explicit internal AST/byte-backed gate, exact
  compiled/product declaration/import/export directories, declaration-based
  policy accounting, duplicate imported-memory alias deduplication, and codec v27
  persistence. A linux/amd64 explicit-bounds staged path executes local/imported/
  re-exported indexed `memory.size/grow`, every scalar and SIMD memory form, active
  and passive data lifecycle, and `memory.init/copy/fill` with exact cross-memory,
  overlap, bounds, and drop behavior. A finite compile-only co-tenant proof now
  serializes owner/consumer basedata, refreshes bounded native directories, and
  admits executable owners, finite imported numeric-global pointer arrays, and one
  bounded imported funcref table under a null/get/set/size-only scan. Retained scalar
  direct imports may now re-enter producers sharing the exact memory-0 mapping through
  stable 256-byte arena images; nested calls compose with imported numeric-global
  pointers and the sole imported funcref table simultaneously while traps, shared
  growth, table state, concurrency, and independent memory/global/table/function
  close ordering remain allocation-free. Host callbacks, foreign-memory/imported-
  tail bindings, local/multiple/unbounded or wider-operation tables, local/reference/
  vector globals, passive/reference tenant state, and live-binding codec persistence
  remain rejected.
  Core 3 compact imports remain strict. The complete 42-file matrix is gap-free at
  913 commands, 79 modules, 771 assertions, 4 invalid, 22 unlinkable, and 20
  uninstantiable cases, with zero feature rejects or blocked commands.
  Snapshot v3 captures and restores every owned
  local memory image/grown size plus passive-data drop state, rebuilding native
  directory entries on restore. Executable-owner/function/private-basedata contexts,
  imported/shared/registered-tenant snapshots, guard mode, public admission, and
  arm64 remain.
- 🚧 memory64 has one bounded linux/amd64 local-or-instance-import execution slice:
  exact 64-bit metadata/codec limits, checked u64 address/offset arithmetic,
  `memory.size/grow`, all 19 integer scalar operations, all four float scalar operations,
  every SIMD memory load/store/extend/splat/zero/lane form, active and passive data
  lifecycle, and `memory.copy`/`memory.fill`. Valid declared maxima through the Core 3
  limit of 2^48 pages persist exactly when the minimum remains allocatable; only the
  direct execution reservation is capped at 65,535 pages. No-maximum declarations retain
  `HasMax=false` under the same finite reserve, unavailable growth returns `-1`, and
  policy/managed accounting rejects overflow fail-closed. One exact non-shared instance-
  exported import preserves provider max/no-max type across re-export, shares growth, and
  retains or rolls back the producer transactionally without growing the lifecycle sidecar.
  The complete sixteen-file non-table matrix is gap-free at 5,904 commands / 169 modules /
  5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked
  commands. Mixed memory32/memory64 imports reject before attachment. Host memory64,
  shared/multi-memory execution, unallocatable minima, guard mode, public admission,
  snapshots, and arm64 remain.
  Table64 is now gap-free across the complete nine-file staged family: 2,802 commands /
  107 modules / 2,600 assertions / 81 invalid / zero gates or blocked commands. Existing
  sole/two-table funcref and local externref execution is joined by exact
  table32/table32/table64 passive init/drop/copy/call-indirect modules with retained
  cross-instance function descriptors. Inert local table64 declarations preserve exact
  u64 maxima through `2^64-1` in inspection and codec v27 while allocating only the
  unobservable minimum; the same capacity split preserves Release 2 oversized inert
  table32 declarations. Exact declaration-only two-local no-maximum and
  `spectest.table64` imported/local products preserve index order, no-max identity,
  zero-minimum descriptors, policy accounting, codec reload, transactional retention,
  rollback, and close-order release. Table64 arithmetic, token identity, traps, and hot
  paths remain bounded and allocation-free. Broader imported copy/init/grow/indirect,
  snapshots, guard mode, public admission, exception handling, GC, and arm64 remain
  end-to-end work.
- 🚧 Exception handling has gap-free strict schema-2 accounting across five official/mixed
  files: 147 commands, 13 modules, 98 assertions, 16 invalid, 2 malformed, 2 unlinkable,
  zero gates, and zero blocked dependents, with zero hidden failures. The complete
  official `exceptions/try_table` file is gap-free under staged admission at 5 modules,
  45 assertions, 9 invalid modules, and 2 source-only malformed commands. linux/amd64
  explicit bounds supports nine tags, twenty-four try tables/module, eight ordered catches/
  table, four nested seven-word handlers, and four fixed three-word exception roots/function.
  Direct/indirect true tails discard dead handlers, and exact retained `() -> ()` cross-
  instance calls transfer catcher basedata plus producer-identity tag matching without
  shared mutable handler state. One exact local-only tag payload may carry a non-null indexed
  `() -> ()` funcref produced by one declarative local `ref.func`: catch/catch_ref/catch-all
  preserve canonical descriptor identity, initialize the root before handler publication,
  clear all three root words on the immediate exn drop, tear down cleanly, retain codec-v27
  metadata, and repeat without allocation. Catch-all root maps derive ownership from the
  bounded tag set and reject mixed scalar/reference or GC/funcref words. The two remaining
  `ref_null` products now execute only null any/none/exn/noexn values and immutable local
  globals through one zero slot; they allocate no collector and do not claim WasmGC heap
  execution. Non-null GC/exn values, allocation/cast/test instructions, foreign, mutable/
  imported, wider, escaping-root, tail, host, snapshot, public, guard, and arm64 products
  remain fail-closed. The scalar catch benchmark is 41.48–41.91 ns/op, the typed-funcref
  catch is 135.1–145.7 ns/op, and bottom-null `global.get` is 52.24–53.58 ns/op; all are
  0 B/op and 0 allocs/op.
- 🚧 WasmGC's complete official `gc/struct.wast` file is gap-free under exact staged
  product admission at 36 commands / 6 modules / 19 assertions / 4 invalid / 1 malformed /
  0 gates / 0 blocked. Its bounded opaque `GCRef` retains exact producer/type/root lifetime
  without exposing compact handles. Iterations 41-43 complete the independently pinned
  `gc/array.wast` path at 61 commands / 7 modules / 41 assertions / 6 invalid / 0 gates /
  0 blocked, with zero hidden failures. The reference leader roots passive inner arrays,
  publishes a fixed two-entry allocation `RootSet`, applies object/card/post-bulk barriers,
  and preserves exact one-live-token ownership. Iteration 44 additionally closes all 80
  `gc/i31.wast` commands: 7 modules and 65 actions execute with no gates or blocked commands.
  i31 values use direct low-bit-tagged arithmetic, literal/imported-global initialization,
  mutable globals, compact 8-byte i31/anyref table lifecycle, and exact casts without creating
  a collector. `ValI31Ref`/`I31Ref` keeps the public immediate category separate from opaque
  `GCRef` object tokens. Codec v27 retains exact type/global/element metadata but inherits no
  staged product or imported-global table-initializer sidecar; snapshots, guard mode, and arm64
  remain fail-closed. Core i31 get and anyref-table get measure 34.63–35.78 ns/op with 0 B/op
  and 0 allocs/op. Iteration 45 pins both `gc/ref_test.wast` leaders and opens a separate
  collector-free null+i31 beachhead. Iteration 46 executes the official 976-byte concrete leader:
  a collector primitive handles null/i31/object categories, struct/array kind checks, declared-
  super traversal, stale/forged/closed rejection, and immutable canonical representatives. One
  exact 168-byte two-slot product first proves checked table roots, repeated barriered overwrite,
  Tiny exhaustion, rejected-write atomicity, close teardown, and codec/snapshot/platform closure.
  Iteration 47 closes the 626-byte abstract leader with a finite three-owner table proof: ten checked
  anyref collector slots, local funcref descriptors, and an eight-entry store/collector-bound extern
  conversion bridge. Public extern tokens, internal foreign-any identities, converted i31s, and
  converted heap objects remain disjoint; object conversion roots are replaced on repeated init and
  all rejected/OOB writes are atomic. Strict `gc/ref_test` accounting is gap-free at 73 commands /
  2 modules / 68 assertions / 0 gates / 0 blocked, with zero hidden failures. The raw conversion
  round trip measures 19.70–21.04 ns/op and the parked foreign-any test 171.7–172.5 ns/op; all report
  0 B/op / 0 allocs/op. Iteration 48 pins and closes the sole 286-byte `gc/extern.wast` leader at
  19 commands / 1 module / 16 assertions / 0 gates / 0 blocked. GC conversion constant expressions
  validate only behind the staged gate; the exact ten-entry anyref table roots a struct and zero-length
  array; and the same fixed eight-entry owner now supplies separate bounded public any/extern identities
  without exposing compact refs or reusing opaque `GCRef` tokens. A 48-byte Tiny heap survives 100
  initializations with exactly two live objects, forged public ingress fails before mutation, codec/
  snapshot/guard/arm64 admission stays closed, and all official host/null/i31/struct/array round trips
  execute. Raw conversion measures 20.96–21.19 ns/op and the stable public round trip 144.2–147.8 ns/op,
  all 0 B/op / 0 allocs/op. Iteration 49 pins and closes the sole 197-byte `gc/ref_eq.wast` leader plus
  six invalid modules. One twenty-entry checked eqref table stores null/i31 values directly and roots four
  distinct struct/array objects; every allocation is stored before the next may-collect helper. An 80-byte
  Tiny heap repeats initialization 100 times, retains exactly four objects, rejects forged/OOB writes
  atomically, and executes all 81 comparisons. Accounting is gap-free at 90 commands / 1 module / 81
  assertions / 6 invalid / 0 gates / 0 blocked. Stable i31 equality measures 45.53–49.41 ns/op, 0 B/op,
  0 allocs/op. Iteration 50 pins and closes both `gc/ref_cast.wast` leaders at 380 and 512 bytes. A new
  allocation-free collector cast primitive reuses dynamic classification, preserves the original compact
  identity on success, and distinguishes the exact `cast failure` trap from null-reference traps and
  stale/forged ownership errors. The abstract leader reuses the ten-slot anyref table plus fixed extern
  conversion owner; the concrete leader reuses the twenty-slot rooted table plus canonical representatives.
  Tiny48 repeats the abstract initializer 100 times with exactly two live objects; Tiny256 retains the
  concrete leader's eight objects. Accounting is gap-free at 47 commands / 2 modules / 40 assertions /
  3 actions / 0 gates / 0 blocked. Stable parked i31 casting measures 177.9–183.8 ns/op, 0 B/op, and
  0 allocs/op. Iteration 51 pins and closes both branch-cast files. Each is gap-free at 40 commands /
  3 modules / 25 assertions / 6 invalid / 0 malformed / 0 gates / 0 blocked. The lowering copies the
  operand only for the non-collecting classification helper, keeps the original 64-bit word on the
  operand stack, and carries that exact identity on the selected edge or fallthrough with correct label
  prefixes, nested ordering, and nullable-target non-null refinement. The abstract products reuse one
  ten-slot checked anyref table and conversion owner, allocate a one-field i16 struct plus a length-three
  i8 array, and repeat in Tiny72 with exactly two live objects. Concrete products reuse the twenty-slot
  table and canonical representatives; nullability-only leaders instantiate separately. Stable parked
  i31 branching measures 124.2–127.0 ns/op, 0 B/op, and 0 allocs/op. Iteration 52 pins and closes
  `gc/array_fill.wast` and `gc/array_copy.wast`: combined accounting is gap-free at 54 commands / 2
  modules / 43 assertions / 7 invalid / 0 gates / 0 blocked. The exact parked helpers never allocate or
  collect; they preflight complete ranges and reference compatibility before mutation, preserve packed-i8
  truncation and memmove overlap, and use object/card/post-bulk barriers with Tiny remark proof. The copy
  product's final `global.set` is followed by a product-gated two-slot cell/root reconciliation; Tiny96
  repeats 100 overlap replacements and retains exactly two current arrays. Packed fill measures
  170.2–173.1 ns/op, 0 B/op, and 0 allocs/op. Iteration 53 pins both array-init files, strengthens
  validation to consume all four operands and require mutable numeric/reference destinations plus exact segment
  compatibility, and closes `gc/array_init_data.wast` at 48 commands / 2 modules / 42 assertions / 2 invalid /
  0 gates / 0 blocked. Its six-word helper preflights destination elements and passive source bytes before any
  write, decodes i8/i16/i32/i64 little-endian values, and never allocates or collects. The three-global leader
  repeats under Tiny96 with all roots retained; the transient width leader repeats under Tiny24. Dropped
  segments preserve exact zero-length success and nonzero traps; source traps are atomic. Stable i8 init measures
  175.4–177.5 ns/op, 0 B/op, and 0 allocs/op. Iteration 54 closes the 268-byte
  `gc/array_init_elem.wast` funcref leader and all 19 return/trap actions. Its non-allocating helper preflights
  both ranges, all selected canonical local descriptor identities, and exact subtype compatibility before any
  write. Two length-12 array globals are checked collector roots; their 64-bit function identities are local
  instance-owned, non-scanned words, so collector object/card barriers are not applicable. Tiny224 repeats 100
  initializations, preserves atomic traps, retains exactly the two arrays across full collection, and honors
  drop plus zero-length-after-drop. Stable element init measures 213.4–219.2 ns/op, 0 B/op, and 0 allocs/op.
  Combined init accounting is gap-free at 72 commands / 3 modules / 61 assertions / 5 invalid / 0 gates /
  0 blocked. Iteration 55 adds complete strict `gc/type-subtyping.wast` accounting: 170 commands, 45 valid
  metadata/function-identity leaders, 24 invalid modules, 8 unlinkable obligations, 45 exact product gates,
  and 48 blocked dependents. Iteration 56 closes all sixteen validator gaps on AST and byte-backed paths.
  Recursive-group equivalence now distinguishes bound from external references; super chains accept equivalent
  recursive projections; function parameters/results enforce contra/co-variance; struct prefixes and array fields
  enforce exact storage, immutable covariance, mutable invariance, and unchanged mutability. No validator allowlist
  remains. Iteration 57 executes the first six declaration graphs and two recursive-function-body leaders through
  a new exact SHA-pinned no-object product rather than widening iteration 37. Iteration 58 adds the next six immutable
  local `ref.func`-global leaders under their own exact class. Their one/two local functions and one/two/four/eight
  globals use bounded 64/96-byte descriptor arenas; every immutable cell holds its instance-owned canonical local
  identity after exact declared-super assignment. Iteration 59 executes four single-result function-only `ref.test`
  leaders. Iteration 60 executes the next three multi-result all-true leaders with 2/4/8 ordered i32 results. One exact
  classifier permits only two or three local functions, one declarative element per tested function, and a runner made
  solely of `ref.func; ref.test` pairs. Iteration 61 executes the final two function-only leaders, each returning zero,
  under a separate exact recursive-chain class. It requires two or three two-member open-function groups whose second
  members point to the preceding group's first member and proves that the tested first member does not inherit that
  sibling super edge in the reverse direction. Compile-only provenance folds every result without treating descriptor
  addresses as compact GC references. Iteration 62 executes the separate 412-byte recursive runtime call/cast leader.
  Its exact immutable three-entry local table carries ordinary canonical descriptor identities; generated
  `call_indirect` and `ref.cast` checks compare those identities against the validated local subtype relation, preserving
  six successful call/cast directions plus three signature and three cast traps without the compact-GC helper. The
  runtime product owns 352 descriptor bytes and a 104-byte
  table image, emits 4,938 code bytes, produces a 5,433-byte codec artifact, and measures 50.78–51.50 ns/op at 0 B/op /
  0 allocs/op. Iteration 63 executes the separate 185-byte finality leader. Its open and final `() -> ()` descriptors
  remain identity-distinct in both directions for `call_indirect` and `ref.cast`, closing two signature and two cast traps.
  The product owns 224 descriptor bytes and a 72-byte table image, emits 1,257 code bytes, produces a 1,555-byte codec
  artifact, and measures allocation-free post-trap local recovery at 37.71–38.02 ns/op. Iteration 64 executes the separate
  186-byte typed-table leader. Its fixed nullable `$t1` table accepts exact `$t1` and subtype `$t2` descriptors under
  `$t2 <: $t1 <: $t0`, executes five widening/exact indirect calls, and preserves two narrowing/unrelated signature traps.
  It owns 192 descriptor bytes and a 72-byte table image, emits 1,431 code bytes, produces a 1,790-byte codec artifact,
  and measures 49.16–52.61 ns/op at 0 B/op / 0 allocs/op. Iteration 65 adds only the first source-lines-486–530 linking
  cluster. A 103-byte three-export provider and 86-byte six-import consumer prove `$t2 <: $t1 <: $t0` across instances;
  three 51-byte narrowing imports execute as expected unlinkables. Provider/consumer wasm/code/codec sizes are
  103/369/623 and 86/0/300 bytes, descriptor arenas are 128/224 bytes, and the provider null-result path measures
  67.56–76.86 ns/op at 0 B/op / 0 allocs/op. Duplicate imports retain one distinct provider, failed later imports roll
  back, and provider-first or consumer-first close releases exactly once. Iteration 66 adds only the source-lines-540–556
  finality link cluster under another exact provider/consumer pair. Its 70-byte provider exports identity-distinct open and
  final `() -> ()` functions; two 38-byte inverse imports unlink without retaining the provider. Provider wasm/code/codec
  sizes are 70/157/323 bytes, each unlinked consumer is 38/0/144 bytes, the provider arena is 96 bytes, and each attempted
  consumer has a bounded 64-byte descriptor requirement. The empty final export measures 36.50–37.43 ns/op at 0 B/op /
  0 allocs/op. Iteration 67 adds only the source-lines-566–572 M3 struct-defined provider/consumer pair. Its two
  two-member recursive groups use an immutable self-referential struct plus an empty companion struct only to determine
  function identity; no struct/array value or opcode executes. The 70-byte provider and 51-byte consumer own 64-byte
  descriptor arenas, retain one producer transactionally across both close orders, and have wasm/code/codec sizes
  70/77/313 and 51/0/236 bytes. Empty `g` measures 38.46–51.80 ns/op at 0 B/op / 0 allocs/op. Iteration 68 adds only
  the source-lines-578–588 M4 struct-projection provider/consumer pair. Its three two-member recursive groups preserve
  exact group/member identity while the final function/struct pair extends different earlier pairs and carries five
  ordered immutable non-null reference fields. The 104-byte provider and 85-byte consumer each own 64 descriptor bytes,
  retain one producer transactionally across both close orders, and have wasm/code/codec sizes 104/77/482 and 85/0/405
  bytes. Empty `g` measures 37.05–39.08 ns/op at 0 B/op / 0 allocs/op. Iteration 69 adds only the source-lines-598–605
  M5 provider/expected-unlinkable pair. Complete recursive-group comparison now preserves member position and bound-versus-
  external references, so the provider's second struct reference cannot flatten into the consumer's self-recursive group.
  The 82-byte provider owns 64 descriptor bytes; the 51-byte attempted consumer has the same bounded requirement but rejects
  before retention or publication. Wasm/code/codec sizes are 82/77/403 and 51/0/236 bytes, and empty provider `g` measures
  36.78–37.82 ns/op at 0 B/op / 0 allocs/op. Iteration 70 adds only the source-lines-614–621 M6 provider/consumer pair.
  Two independent self-recursive function/struct groups remain distinct, while the final `g <: f1` edge links exactly.
  The 82-byte provider and 63-byte consumer each own 64 descriptor bytes, retain one producer transactionally across both
  close orders, and have wasm/code/codec sizes 82/77/403 and 63/0/326 bytes. Empty `g` measures 37.44–42.95 ns/op at
  0 B/op / 0 allocs/op. Iteration 71 adds only the source-lines-628–639 M7 provider/two-import consumer pair. The
  fourth-group `h` extends the provider projection and satisfies both consumer `f1` and `g1` views. Provider/consumer
  descriptor arenas are 64/96 bytes, duplicate imports retain one producer, wasm/code/codec sizes are 114/77/561 and
  102/0/502 bytes, and empty `h` measures 36.65–38.72 ns/op at 0 B/op / 0 allocs/op. All thirty-eight admitted leaders
  leave `Instance.gc` nil. Codec reload inherits no product marker; snapshots and guard/public/arm64/host admission remain
  closed. Accounting is 38 passed modules / 23 passed assertions / 7 gates / 12 blocked dependents / 24 invalid / 6
  executed plus 2 blocked unlinkable obligations. General frame roots, object-valued mutable/reference globals, later
  linking clusters, the non-flat export, broader typed-table ownership, public family admission, and broader platforms remain.
- [x] Reach zero unexplained failures/skips in the official Release 3 core suite.
  The pinned `wg-3.0` corpus now passes all 2,226 modules and 58,038 assertions
  with zero failed/skipped modules or assertions and zero compile, instantiate,
  unavailable-module, missing-export, reference-argument, reference-result, or
  reference-global gaps. `CoreFeaturesV3` is the implementation ceiling while
  the default configuration remains Release 2-compatible; the spec harness opts
  into Release 3 explicitly.

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
- 🚧 Interruption / cooperative cancel: ARM64 `Call(ctx)` polls at function
  entries and loop headers and returns `context.Canceled`/`DeadlineExceeded`;
  amd64 native polling remains planned. The checkpoints also bound ARM64 Go-GC
  stalls during long native loops.
- [ ] Wasm-level stack traces on trap (trap site → func idx → wasm pc)
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
- [ ] `.wago` productization: cache keys (module hash + compiler version + CPU features
  + bounds mode + ABI) and a compile/run/inspect CLI

## Verification & quality

- [ ] Differential oracle: fuzz modules, compare results/traps against C++ WARP (the
  off-path `src/core/compiler/ir` package is reserved as this oracle)
- [ ] Byte-for-byte codegen diffing against WARP for shared inputs
- [ ] Golden disassembly regression net (grows one golden per optimization from P1 on)

## Bigger bets

- [x] SIMD (`v128`) — complete for the documented linux/amd64 SSSE3/SSE4.1 + AVX/VEX.128 baseline: every decoded core SIMD opcode and deterministic relaxed SIMD opcode through 0xfd 275 is frontend-admitted, validator-admitted, and lowered by railshot; reserved proposal-table holes are invalid-decode tests. Public `[16]byte` (`wago.V128`) plumbing covers locals, params/results, control flow, globals, cross-instance imports, and host imports/results. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). Keep AVX2/FMA/VNNI optimizations behind future CPU gates. Current metrics: [`docs/simd-performance-2026-07.md`](docs/simd-performance-2026-07.md).
- [ ] Threads & atomics
- 🚧 Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`):
  decoder/validator foundation plus amd64 local register- and wrapper-ABI direct,
  tail-position host imports, private-immutable-table mixed indirect, same-instance
  local typed-reference, and retained cross-instance root/nested typed-reference
  frame-reuse milestones exist. The pinned `return_call` file is fully green at 47
  commands / 3 modules / 33 assertions / 11 invalid. The 79-command indirect file
  is fully green at 3 modules / 49 assertions / 16 invalid / 11 malformed. `return_call_ref`
  is gap-free at 51 commands / 5 modules / 35 assertions / 11 invalid, including one
  canonical funcref result. Retained integer plus exact `(i32, f64) -> f64` and
  `(f64) -> i32` cross-instance direct tails use a separate fixed root/nested return transition. Public
  admission, other float/oversized direct tails, general-table/foreign-float/general reference-result
  tails, snapshots, and arm64 execution remain.
- [x] Basic extended constant expressions: integer add/sub/mul, prior immutable
  globals, active offsets, strict validation, and codec-v27 persistence.
- 🚧 Typed function references: typed `ref.func`, recursive structural equivalence,
  and staged indexed-signature frontend admission now reach amd64 descriptor
  `call_ref` with null/signature checks and wrapper/context-aware non-tail calls.
  Public structural descriptors and codec-v27 exact metadata cover signatures,
  globals, tables, elements, imports/exports, and inspection without enabling the
  feature. Staged exact storage/import compatibility, indexed/recursive runtime
  signature identity, bounded collision-resistant native type keys,
  `ref.as_non_null`, both null branches, exact public/host funcref boundaries,
  and non-null harness result matching are now proven.
  Mutable global host/public boundaries now enforce exact indexed types and
  nullability, and shared table/global producer roots release on successful final
  overwrite without violating trap atomicity. Cross-instance typed descriptors now
  retain their producer through consumer close, and typed/tail opcode requirements
  survive codec metadata while snapshots reject unresolved contexts before mutation.
  Root and nested cross-instance typed tails now execute with explicit host and
  unsupported-shape failures. The 14-file schema-2 typed-reference/structural matrix is
  gap-free at 422 commands / 61 modules / 246 assertions / 65 invalid / 2 malformed /
  2 unlinkable, with zero gates or blocked commands. The null-control surface, official
  `call_ref` file, and all valid `type-rec` products are green under staged admission;
  shifted and recursive cross-instance signatures match structurally, retain their
  producers, and preserve codec-v27 metadata across empty recursive groups. Iteration 31
  closes all five strict recursive validator gaps by enforcing recursive-group scope and
  whole-group equivalence. Iteration 36 executes both null-only mixed GC/EH modules without
  allocating heap objects. Iteration 37 makes the complete matrix gap-free by admitting ten
  exact `type-rec` products where struct definitions affect only function identity. Struct
  descriptors survive codec v27, but an exact compile-only product proof keeps the instance
  collector nil; no struct/array opcode or value is admitted. Persisted live reference state,
  broader tails, public admission, actual GC allocation/access, remaining reference/GC/EH
  instructions, and arm64 remain gated. Multi-
  memory now executes all indexed scalar, SIMD, and bulk/data operations internally
  on linux/amd64 explicit bounds, decodes compact import groups, accounts for all
  913 commands in the complete 42-file family matrix, snapshots owned local memory
  sets through snapshot v3, and stages bounded registered-memory co-tenants. The
  serializer now admits imported numeric globals, one bounded imported funcref table,
  and retained scalar direct calls to exact same-memory producers through recursive
  stable-image transitions; numeric-global pointers and the sole imported table are
  jointly proven in the same root/nested native re-entry chain, while host/foreign/tail
  imports and broader reference state remain explicit gates. Memory64 now has one
  bounded local-or-instance-import size/grow/scalar/SIMD-memory/active+passive-data/
  copy/fill slice with exact metadata/codec limits, checked u64 arithmetic, overlap,
  drop state, trap atomicity, finite execution reservations, exact address/max-form
  import rejection, and gap-free complete sixteen-file accounting. Valid declared maxima
  through 2^48 persist exactly while only executable reserve is capped. Table64's
  complete nine-file staged matrix is gap-free: sole/imported funcref operations,
  two-table mixed-width operations, local externref forms, and retained-function
  table32/table32/table64 init/drop/copy/indirect all execute through the native
  directory. Exact u64 maxima through `2^64-1` persist in codec-v27 metadata while
  inert declarations allocate only their minimum; declaration-only two-local and
  `spectest.table64` imported/local no-max products preserve lifecycle and index order.
  Imported/shared snapshots, broader imported copy/init/grow/indirect, guard mode,
  public admission, exception handling, and WasmGC remain active scope;
  see `docs/wasm3.md` for exact boundaries.
- [x] Reference-types product completion: signatures, locals, control,
  local/imported/shared globals, host ABI, explicit host funcref ownership/egress,
  typed 8-byte externref tables/elements, every `table.*` operation, multiple
  local/imported tables, exact exports/re-exports, codec-v27 structural metadata,
  snapshot isolation, complete inspection, cross-link teardown, and the
  zero-skip Release 2 execution corpus are done.
- 🚧 Additional targets: native **linux/arm64** and **darwin/arm64** backends and
  runtime paths are implemented and under qualification; Windows ABI support
  remains planned.
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals (for now)

- An interpreter tier (wago is single-pass JIT only)
- **An SSA / IR execution tier** — decided against 2026-07-03; railshot is the one and
  only backend, and the ceiling is attacked incrementally instead
  (see [docs/no-ir-plan.md](docs/no-ir-plan.md) §0)
- Re-implementing WARP's linker/disassembler/fuzzer (they live in `warp/` as the
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
