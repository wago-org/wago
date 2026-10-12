# Wago roadmap

Experimental Linux/RV64 now combines a bounded owned/imported funcref table32
with numeric function imports, including retained foreign targets and parked
host callbacks, imported references through owned thunks, and bounded nullable
funcref element expressions with immutable imported-i32 offsets. Scalar/SWAR
initialization supports numeric/vector imported-global copies and imported-i32
data offsets, with bounded integer constant add/sub/mul expressions. Immutable
nullable funcref globals support local copies, exports, and explicit same-Runtime
imports into modules with no table, an owned table, or a host-created imported
table. Nullable funcref locals/control and body-level `ref.func` of export-declared
local or native-imported numeric/vector functions also work without module state.
Local mutable nullable funcref globals retain only own canonical descriptors or
null; their profile excludes tables, reference imports/signatures, and mutable
reference exports. Parked callbacks, retirement and compiler-free artifact tests
cover this bounded shape.
The exact official concrete struct-subtyping `gc/ref_cast` module now has genuine
RV64 lowering for indexed heap types 0 through 7, retained source/artifact
classifiers, rooted-table GC stress coverage, and compiler-free reload. Baseline
RV64G QEMU establishes functional correctness only; native cache/performance
qualification remains pending, and other module shapes remain closed.
The exact official abstract `br_on_cast` and `br_on_cast_fail` products now have
genuine RV64 lowering for nullable/non-null i31, struct, array, and indexed branch
targets, including typed multi-value label transfer. Exact retained-metadata
classifiers, source/artifact stress-GC execution, dirty-upper i32 table indices,
out-of-bounds traps, and compiler-free reload are covered. This checkpoint is
functional QEMU qualification only; native cache/performance qualification and
broader branch-cast shapes remain pending.
The exact official concrete struct-subtyping `br_on_cast` and
`br_on_cast_fail` products now lower indexed heap targets 0 through 7 through
the rooted four-slot subtype helper, including canonical-equivalent and declared
supertype cases. Source and compiler-free artifact execution, retained metadata,
stress collection, and the eight rooted table objects are covered under RV64G
QEMU. The two remaining official nullability-only branch-cast products now also
have genuine framed RV64 lowering, exact source/artifact admission, collector
lifecycle coverage without table state, and compiler-free reload. Native
cache/performance qualification and broader non-official branch-cast shapes
remain pending.
The first six official `gc/type-subtyping` modules now have exact source and
compiler-free artifact admission for their declaration-only array, struct,
function, self-recursive, and mutually recursive subtype graphs. Retained
type/descriptor metadata is validated exactly, while instantiation allocates
neither executable memory nor a collector. The two immediately following
official modules now form a separate exact RV64 recursive-function checkpoint:
three private reference-typed functions receive framed direct-call code across
self- and mutually recursive declared function groups. Exact source and
compiler-free artifact classifiers bind the flattened type graph, descriptor
graph, function/type keys, entries, features, ABI flags, and code digest before
executable publication. Instantiation is collector-free, and close tests cover
the mapped-code lease. Because neither module exports a function, this is not
evidence for a public recursive-call result. Functional qualification is under
baseline QEMU RV64G; native cache and performance qualification remain open.
The next exact collector-free checkpoint covers official modules `.8`, `.9`,
and `.11` through `.14` (`.10` is invalid): one or two genuinely framed local
RV64 functions back one, two, four, or eight immutable indexed `ref.func`
globals. Exact source and decoded shapes plus code and serialized retained-
metadata digests bind recursive type graphs, function/type keys, global target
indices, value types, features, and ABI flags. Source/artifact instantiation,
actual compiler-free reload and source rejection, W^X publication, executable
leases, and final unmapping are qualified under baseline QEMU RV64G. The six
modules have no exports, and native cache/performance qualification remains
open. Official `.17` is now a separate exact callable checkpoint. Its three
private function-subtype descriptors populate an immutable table; genuine RV64
`call_indirect` admits only the pinned transitive subtype keys and genuine
`ref.cast` traps on non-matching full-width canonical descriptors. Source,
artifact, actual compiler-free, W^X lease, final-unmap, success, and six failure
paths are covered under baseline QEMU RV64G without a collector. Cross-instance
subtype boundaries remain closed for later checkpoints. Official `.18` now has
its own exact collector-free checkpoint: structurally identical open and final
function types are separated by pinned private descriptor keys, and both
wrong-signature indirect calls plus both cross-finality casts trap from source
and compiler-free artifacts. Exact retained metadata, W^X publication, live
leases, final unmapping, and framed trap re-entry are covered under baseline
QEMU RV64G. Later `gc/type-subtyping` runtime products and native cache/performance
qualification remain open.
Table-free declarative segments, empty/state-only modules, bounded opaque custom
metadata and empty supported sections are admitted. State-only modules retain
resource ownership without allocating executable memory. The public pinned SIMD
suite includes all 472 valid modules and checks 669 binary invalid cases; its 510
malformed WAT text cases remain outside the binary-only API harness. Table-free
passive segments and immutable reference-global element initializers preserve
canonical owners. Bounded multi-memory lowering covers scalar, FP, SWAR, bulk
operations and aliased imports. Memory64 adds per-memory address typing,
carry-checked scalar/SWAR accesses and growth, mixed-width bulk operations and
I64 initialization while retaining finite physical reservations. Fallback
control nesting supports 128 constructs. Pinned integer/conversion, float,
memory, control and complete factorial/call corpora extend public source/artifact
qualification, including exact stack-exhaustion assertions, and
diagnostics expose finalized code/frame statistics. Profiling builds now retain
bounded finalized regions and optional sparse source maps, including deferred
scalar origins and cold traps; wrapper/helper bytes remain unattributed.
Unwind maps and detailed compiler-site attribution remain pending.
A private qualification profile now exercises proper local, native-import,
indirect and typed-reference tail calls with numeric/vector signatures, plus
synchronous scalar Go and owned-host targets. Certified owner snapshots include
import-only modules without claiming a guest frame for their wrappers. Tests
cover ordinary `call_ref`, mixed call paths, nested host reentry, table replacement
while parked, cancellation, exact quotas and compiler-free artifacts. Deep
prepared-session tests execute 20,000 dynamic tails ending at a host callback
on the minimum native stack. Internal `ref.as_non_null`, `br_on_null` and
`br_on_non_null` preserve the full reference carrier and share a retained-carrier
artifact requirement. Source and compiler-free tests cover null traps, unreachable
uses, deep typed label transfers, numeric loop parameters, combined `call_ref`,
and retained foreign immutable globals. Existing non-null artifacts retain the
same format and load without recompilation. Explicit abstract function-reference
block results, typed selects and initialized non-null locals are qualified through the same
retained-carrier contract. Nullable spellings retain ordinary ReferenceTypes
admission; non-null type-only bodies carry the typed feature and artifact witness
even without typed opcodes. Frontend initialization checks reject invalid locals
before image staging. Non-calling reference control does not reserve declared-call
transport. Selects preserve both full-width reference arms while zero-normalizing
i32 conditions, including raw incoming upper bits. Standalone indexed `ref.null`
constants within the admitted numeric function-type directory carry the typed
feature and retained-carrier witness even in unreachable bodies. Abstract
`ref.null func` retains baseline admission; neither form reserves declared-call
transport. Public TailCall and
TypedFunctionReferences remain
gated: these bounded cases do not establish complete feature support.
Internal indexed function-reference locals, inline block results and typed
selects within the existing numeric/vector type directory are also qualified
through source and compiler-free artifacts. Both nullable and non-null indexed
annotations carry the typed witness, including unused locals and unreachable
bodies. Matching signatures, nullability and initialization are proved by the
frontend before carrier erasure. Real descriptor selection, large-frame local
reloads and foreign imported-function owners through joined teardown are
covered. Scalar untyped selects now preserve unknown carriers in unreachable
code, matching the SWAR path; typed selects and reference branches specialize
them without accepting concrete incompatible operands or borrowing below a
nested control floor. Source and compiler-free artifact fixtures cover admission
and exact unreachable traps followed by instance reuse.
Indexed control signatures now preserve reference parameters/results
through exact joins, loop backedges and overlapping scalar/SWAR transfers.
Unreachable branch tables check each target against the original polymorphic
payload; known incompatible values still reject. Signature budgets precede
derived carrier allocation. Source and retained-artifact admission distinguish
internal controls from numeric/vector callable signatures, including unused
declarations. Scalar and SWAR decoded budgets charge reference encodings before
validation. Typed declarations without a defined code owner remain closed pending
a separate empty-code ownership contract; reference-valued callable signatures
also remain closed. A separate private same-image direct-call mode
preserves reference entry/results and mixed scalar/SWAR transfers, including
explicit root exits, recursive stack traps/reuse and maximum compact arenas.
Separately enabled local proper tails preserve exact reference result types,
original return addresses, constant entry SP, and cooperative cancellation across
mixed reference/vector transfers. Private decoded scalar/SWAR module adapters
now require a bounded closed-module proof: numeric exports and descriptor targets,
numeric function imports only, and no indirect calls or reference tails. Ordinary
numeric reference calls now compose through the bounded retained bridge. They retain actual function
indices across descriptor-directory holes. Legacy production staging routes and
legacy isolated instantiation reject that mode before allocation. A private
sparse owner constructor now retains full physical function spans with explicit
noncallable ordinals, empty descriptor/target rows and bounded frozen metadata.
Callable targets after internal functions preserve their indices; entry binding
also works when ordinal zero is internal. A separate backend staging permission
now selects only actual reference-valued functions, rechecks closed source and
all emitted modes, and records a distinct image witness. Scalar/SWAR direct calls
and local proper tails retain framed ABI and bounded metadata. Bare nullable
signatures keep baseline ReferenceTypes admission; ordinary modules and unused
control declarations keep their existing witnesses. Private source and retained
artifact integration now preserves that witness, checks the closed metadata
allowlist, and requires destination permission before RW staging becomes RX.
Sparse production owners retain internal frame spans without callable identities;
ordinary entry checks every internal descriptor and target row is empty. Tiny
numeric foreign exports can invoke wider internal reference/vector functions
after logical producer closure. Closed internal reference functions also compose
with owned numeric globals, unshared memory32/64, multiple memories and bulk data.
Source and retained admission bound state and types together; exact export maps
and canonical initializer forms preserve confinement. Scalar/SWAR tests cover
stateful calls and local tails, high-bit initializer bounds, failed stores without
partial writes, exact generation quotas and all-memory retirement after foreign
callers close. Owned numeric resource exports now have exact shared namespace
validation, aggregate name/count budgets and alias sharing. Borrowers can grow an
exported memory even when the producer contains no growth instructions. Independent
memory-only/global-only borrowers retain all storage after function callers close;
late imports and host state access through a closed producer reject. Resource
imports now compose in this closed mode with separate function/global/memory
namespaces, exact names and zero imported initializer payloads. Non-primary
memory aliases refresh on host resume before guest growth; numeric resource-only
providers remain physical attachment roots without callable snapshots. Source,
retained metadata and compiler-free paths cover resource aliases, full-u64 active
initializer rejection, failed-link refunds and joined callback interruption.
General reference-valued imports/exports and mutable reference ownership remain
separate work. Ordinary numeric function imports now
compose with private references across native calls and scalar Go/owned callbacks.
The imported prefix preserves physical ordinals, exact numeric bindings and
paired internal descriptor holes; sparse generations retain native reexports and
host identities through producer retirement. Exact limits, interrupted callback
cleanup and compiler-free artifacts qualify this bounded path. Numeric direct
imported proper tails now compose with private reference frames, preserving
ordinary-caller continuation, scalar/SWAR payloads and static capability lifetime.
Exact finalized plans, numeric target results and real frame extents are checked
before publication; internal source rows remain noncallable. Baseline RV64G
source/artifact tests cover foreign homes, native reexports, parked alias growth,
joined interruption, static quota rejection/retry, warm native allocation and
deep local tails followed by an imported tail on the minimum stack. Native vector
targets are supported; V128 Go host signatures remain unsupported. Defined numeric
void starts now compose with private reference functions and direct imported tails,
with canonical source/retained selectors, real framed-entry validation and bounded
metadata accounting. Construction, callback scope, nonnil operation reservations,
physical owners and static capabilities survive nested reentry and parked growth.
Start-once, nonzero/final ordinals, trap/panic/initializer rollback, cancellation,
Close joins and exact quota rejection/retry have focused source/artifact coverage.
Canonical numeric imported void starts now compose with the selected mode,
preserving separate import namespaces and the defined-start frame proof. Shared
and owned Go callbacks use scoped construction authority without a fabricated
native outer frame; native callbacks identify the original producer, including
reexports, and retain full-width transport. Start-once, nested private guest
entry, cancellation/Close joins, genuine native traps, code retirement, linking
before publication and exact quota retries have source/artifact coverage.
Owned mutable nullable funcref cells now compose with private functions, confined
to null or own numeric descriptor slots, including imported numeric targets.
Complete bounded source proofs preserve scalar256/SWAR128 limits and reject
private initializer targets, reference imports/exports and dead escapes. Retained
exact-type/pool/literal checks, full-width distinct saved/lower snapshots after
callback clearing, start and foreign reentry, parked retirement/Close joins,
aggregate and physical quotas, compiler-free execution and zero-allocation warm
local calls qualify the bounded composition. No mutable foreign-owner edge or
cycle collector is introduced.
Own immutable plain nullable funcref cells also compose with private scalar/SWAR
functions, with exact null/numeric literals and preserved mutability. Complete
source and retained bounds, table-free null headers/full numeric directories,
dead/nested immutable-write rejection, real cell-produced indirect tails,
native/Go imports, parked callbacks, retirement, quotas and compiler-free
artifacts qualify this slice. Immutable cells remain instance-owned storage;
general reference imports/exports still require the separate ownership work.
Ordinary numeric `call_ref` now composes with the selected scalar/SWAR private
frames and own mutable cells. Numeric own/native/Go targets stay canonical; private
reference-bearing rows remain noncallable. Dead target confinement, frontend-backed
operand-floor checks, exact staging witnesses, ordered transport at existing
scalar/import/vector bounds, private trap attribution and reuse, retired relays,
foreign dynamic roots, quotas and zero-allocation own calls qualify this slice.
One bounded private owned nullable funcref table32 now composes with these frames
and cells through ordinary numeric `call_indirect` and table.get/set/size. Active
legacy numeric elements and literal null/own numeric table initializers stay
confined even when unused; exact empty export metadata blocks host ingress.
Source and retained aggregate bounds, sparse SWAR keys, table-only pooled types,
strict frontend-backed dead operands, dirty-upper indices, private trap/reuse,
maximum ordered transport, no-max reserved capacities, starts, retired relays,
parked cleanup and compiler-free artifacts qualify the new slice. Bulk table
init/drop/copy/grow/fill now compose through the same confined private profile,
with bounded passive numeric elements, original segment indices and
per-instance drops. Active/passive payloads share the 1024-value and aggregate
byte budgets. Strict scalar/SWAR operand floors, complete range guards, overlap,
growth-before-length publication, exact private trap/reuse and interruption
checks qualify this slice. No-max growth requires minimum<=512 with at most 1024
reserved entries. Proper numeric `return_call_ref` now composes with this
private scalar/SWAR profile using canonical own/native/scalar Go targets and
empty private descriptor rows. Actual instruction witnesses select one dynamic
factory for mixed direct/reference sites; complete finalized and retained proofs
bound declared results, actual frames, argument spans and full-width target homes.
Maximum scalar256/vector128 transport, repeated private-frame cycles on the minimum
stack, authentic null/signature traps and reuse, active target parking with nested
reentry, cancellation/Close joins and exact dynamic quota retries qualify this
slice. Frozen actual keys and complete owner tuples authenticate targets before a
genuine declared-signature mismatch traps. Old non-reference-tail fixture bytes
are preserved, and cold root completion retires generations; only warm generation
activation claims zero allocation. Proper numeric `return_call_indirect` now
composes with the confined private owned table32 and the same factories. Separate
actual reference/indirect witnesses require an emitted plan for each used kind;
exact table/type/source/frame/home proofs preserve old no-indirect images. An
untyped-only private indirect module needs no typed-reference permission. Canonical
wrong-signature/null/bounds traps, distinct type/function/table ordinals, far
full-width maximum homes, active target parking and exact dynamic quotas qualify
source, retained artifacts and compiler-free execution. Broader table ingress and
reference external signatures remain gated. Confined nullable funcref literal
expression elements now compose with these private frames. Every original null
or numeric function literal is proved before discarded declarative payloads are
removed. Exact retained element type-pool bindings include empty/dropped slots;
final declarations without retained slots do not intern unused types. Tableless
declarative drops are bounded and idempotent; tableless active state stays closed.
Complete null replacement, expression-installed own/native/Go targets, original
ordinals, exact aggregate bounds, compiler-free artifacts and retained codec
rejection/repair qualify the slice. Closed typed-reference functions may also
retain bounded, nonempty legacy passive declarations of numeric targets; the two
executable modules from the pinned Core 3 `br_on_null.wast` now cover null/taken
paths, `call_ref`, traps, block arguments and dirty-upper i32 ingress. This is
complete file-level qualification for that pinned test: the same file's exact
declaration-only module is now admitted separately: its three unexported and
unreferenced functions cover nullable indexed funcref, abstract funcref and
abstract externref `br_on_null` validation without publishing a callable externref
ABI or descriptor. Imports, exports, start, state/resources, altered signatures
or bodies, tableless active and typed-expression passive shapes remain closed.
The file's remaining invalid module is pinned to its exact validation failure;
the broader official typed-reference corpus remains unqualified on RV64. No new
owner graph or ABI is needed. The first executable module from pinned Core 3
`ref_as_non_null.wast` also qualifies the exact table-free numeric-export shape
through source and compiler-free artifacts, including unreachable-before-null
trap order, full-width internal carriers and normalized i32 results. This does
not qualify typed-expression or active elements, tables, imports, resource or
reference-valued public/imported signatures, or general ownership beyond the
pinned file. The exact 59-byte no-debug `ref_as_non_null.2.wasm`
declaration module is qualified separately through exact wire, decoded and
retained classifiers. Its three private bodies only validate and drop non-null
indexed, func and extern parameters; it adds no callable externref boundary,
ownership protocol, resources or name/custom metadata. Together with the
file's exact invalid module and executable assertions, this completes
file-level qualification for the pinned `ref_as_non_null.wast`.
The exact 59-byte no-debug `ref_is_null.1.wasm` declaration module is also
qualified through separate wire, decoded, native-code and retained-artifact
attestation. Its three private functions observe non-null indexed, func and
extern parameters and immediately drop the numeric result; they add no
callable reference boundary or resource ownership. The file's exact `.2` and
`.3` invalid modules are pinned to their typed validation failures. The
table-backed, externally callable `.0` module remains closed, so this is not
yet file-level `ref_is_null.wast` qualification.
The first and third executable modules from pinned Core 3
`br_on_non_null.wast` now qualify separately through source and compiler-free
artifacts. Exact WABT 1.0.41 bytes from revision
`9d36019973201a19f9c9ebb0f10828b2fe2374aa` cover null fallthrough and
non-null branching, internal `call_ref`, the nullable join followed by
`ref.as_non_null`, unreachable-before-branch trap order, multi-value block
arguments, dirty-upper i32 normalization, full-width private descriptors,
owner-scope reuse and cleanup. This does not qualify active or typed-expression
elements, imports, resources, reference-valued public signatures or general
ownership beyond the pinned file. The same
pinned file's exact 75-byte `br_on_non_null.1.wasm` declaration module now
qualifies separately. Its three private, unreachable functions cover non-null
indexed-function, abstract-function and abstract-extern block annotations; an
exact whole-module witness confines abstract extern to the typed `0x64` block
form and keeps selects, locals, results and callable signatures closed. The
canonical 106-byte debug-name form, altered bodies or metadata, and all
imports, exports, starts, state and resources remain rejected. This validates
decoding, lowering and source/artifact confinement only; it does not add a
callable externref or ownership contract. With the exact invalid module pinned
to its validation failure, these three valid modules and the invalid module
complete file-level qualification for the pinned `br_on_non_null.wast`; they
do not imply broader typed-reference corpus parity.
Mixed table/call_ref and private mutable-cell fixtures retain
foreign owners through retirement without extra control-only call transport.
Conservative resource-root accounting and an independently tested bounded receipt
primitive prepare later reference ownership work. Native function, memory, global and table
imports now record their actual producer acquisitions in one process graph and transfer
receipt ownership from construction to instance teardown. Cleanup retires graph
members only after physical callbacks finish. This noncollecting prerequisite
keeps the physical-root shadow entirely external; its prototype metadata ceiling
and exact anonymous fallback do not qualify complete registration or collection.
Memory rows also preserve exact per-handle importer counters and protect consumer
resources through concurrent transfer, counter consumption and producer callbacks.
Global rows also keep counted cell owners separate from frozen descriptor owners,
preserve aliases and host-only counts, and hold consumer resources through
concurrent transfers and producer callbacks. Table rows preserve the acquired store,
actual storage owner, host topology and unconsumed counter obligations; acquisition
precedes native construction and adoption follows construction-lease release.
Frozen immutable-global descriptors now have separate actual-owner receipts,
with guarded registration, alias sharing, builder rollback and terminal cleanup.
Mutable-container acquisitions still require receipt integration, followed by
scalable complete registration and cycle collection. Feature admission is unchanged.
Remaining work
includes reference-global imports combined with instance-owned imported tables,
general mutable reference imports/exports, reference
signatures/equality, broader WebAssembly parity, and native cache/runtime/performance
qualification. The
experimental build tag and unsupported-feature admission remain in place.
- [x] Wide Arithmetic Phase 4: automatic baseline AMD64, ARM64, and experimental
  Linux/RV64 lowering for i64 low/high pairs, including validation and native
  artifact reload. RV64G QEMU establishes functional correctness; native RV64
  cache and performance qualification remain pending.

AMD64 uses SSE2 as its architectural baseline. Newer CPU extensions are optional
compile-time optimization tiers and are recorded in native artifact requirements
when emitted. Scalar, core SIMD, and supported relaxed SIMD have baseline
fallbacks. The `wago_amd64_sse2` build tag selects portable baseline code generation
on modern hosts. TinyGo includes both baseline and modern lowering within its
existing release-size budget.

Baseline and modern profiles pass the official core SIMD corpus (24,325
assertions each) and the supported relaxed SIMD corpus (69 assertions each).
The forced-baseline public API suite and instruction-decoder checks cover
fallback execution and exclusion of optional instructions. Standard Go, TinyGo
and guard-page tests pass. The exact release-size check passes all four profiles
with the CI toolchain (Go 1.22.12 and TinyGo 0.41.1). Shared instruction selectors
and encoders reduce the TinyGo profile from 2,372,584 to 2,351,760 bytes, leaving
240 bytes under the unchanged 2,352,000-byte budget. Both CPU tiers remain enabled.

Focused performance measurements on 2026-09-26 compared `1f137e8e6` with
`aabccddf1` on a Ryzen 7 8845HS using Go 1.27.1. Eight 250 ms samples per workload,
one Go worker, fixed CPU affinity and alternating profile order found no
statistically significant modern-path change across 16 workloads. Geometric
means of median ratios were +1.83% for compilation and −0.99% for execution;
other programs remained active, so these are not improvement claims. Modern code
sizes, checksums and compilation allocations matched main. All execution
profiles measured zero allocations. Larger baseline SIMD sequences remain
candidates for code-size and execution-time optimization.

The size repair was also compared with PR head `15e7e116a` across the same 16
workloads and both CPU tiers. Six 150 ms samples with alternating order and fixed
CPU affinity found no statistically significant slowdowns. Modern compilation
changed by −0.73% and execution by approximately 0.00%; baseline compilation
changed by −0.68% and execution by −0.14% (geometric means of median ratios).
Native code bytes, checksums, B/op and allocs/op were unchanged in both tiers.
These small timing differences are not speedup claims.

Wago is a pure-Go, no-cgo, single-pass WebAssembly engine. It is a from-scratch
port of [WARP](https://github.com/wago-org/warp)'s design. Linux, macOS, and Windows
on amd64 and arm64 are supported. The amd64 backend uses the architectural CPU baseline
of SSE2, with optional SSSE3/SSE4.x and AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI remain
outside the baseline and require explicit feature gates.

## Start here

This file tracks completed work, active work, and longer-term ideas. For users,
[FEATURES.md](FEATURES.md) is the source of truth for support on a selected
build. For maintainers, use [Current work](#current-work-near-term) to find the
active tracks. The later iteration sections are historical completion records;
they keep the evidence behind current feature status.

## Related files

These documents give more detail:
- [FEATURES.md](FEATURES.md) — the per-feature support matrix (source of truth for
  spec-feature status).

Status: [x] done · 🚧 in progress · [ ] planned.

## Done

This section is a summary of delivered capabilities. It is not a compatibility
promise for every build; check [FEATURES.md](FEATURES.md) for exact support.

**Full WebAssembly 1.0 (MVP).** The pinned pre-reference-types spec testsuite passes
in full — 57/57 applicable files, 0 failing assertions (see [SPECTEST.md](SPECTEST.md)).

**Frontend (`src/core/compiler/wasm`)**
- [x] Binary decoder for all sections; byte-backed `DecodeModule` (function bodies
  stay raw bytes, not materialized AST)
- [x] Full validator (operand/control stack typing), byte-backed and differential-tested
  against the official spec testsuite; independent function bodies support bounded,
  deterministic parallel validation through the function-worker policy

**Compiler backend (`src/core/compiler/backend/railshot`)**
- [x] Single-pass amd64 and arm64 codegen with the WARP Valent-Block register
  allocator (symbolic operand stack, deferred-action trees,
  whole-register-file allocation, spill-to-canonical-slot)
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
  trap→error, zero-copy linear memory. Native stack capacity is configurable from
  512 KiB through 1 GiB while retaining the 4 MiB default, 256 KiB fence, exact
  engine-cache matching, and equal-capacity synchronous host re-entry stacks.
- [x] Cross-instance linking: function / global / table / memory imports & exports,
  including shared mutable tables + memories. Imported calls compile once and bind
  through per-instance dispatch cells with explicit direct/indirect context switching.
- [x] Instance slot reuse (lower instantiate cost — explicit #105, guard-page #108)

**Component Model**
- [x] Decode and instantiate Preview 2 components with canonical ABI lift/lower,
  typed host linking, resources, composition, and safe adapter-mediated host
  re-entry. The component runtime is exposed as the capability-gated
  external [`wago-org/component-model`](https://github.com/wago-org/component-model)
  plugin; WASI host capabilities remain in the separate `wago-org/wasi` module.

**Tooling**
- [x] `wago` CLI: `run` / `validate` / `version`, typed args, automatic selected
  Core 3 defaults on complete backends, and explicit `--core 2` / `--core 3`
  release selection
- [x] Public API: `Run`/`RunValues`, `Compile`/`Compiled`, `Instance`, plus
  opt-in serial/adaptive/forced function-worker policy for validation and codegen
- [x] Workers plugin: the separate `github.com/wago-org/workers` plugin
  owns a transactional worker service with bounded copied tagged delivery,
  cooperative kill, neutral exit events, and creator-authorized lifetime links;
  actor/PID/mailbox/supervisor policy remains plugin-owned
- [x] `wago run` and `wago validate` expose adaptive/forced function workers via
  `-p`, with serial defaults for predictable memory use
- [x] Benchmarks vs wazero (compile ~34× faster; wago wins fib_rec, sieve, memory_tree,
  linked_list, dispatch, branches, json deserialize; loses on json serialize, blake)

**Arm64 acceptance (done)**
- [x] Parent/child corpus runner with hard per-case deadlines and explicit/guard/wazero outcomes
- [x] Darwin/arm64 guard-page execution via synchronous SIGSEGV/SIGBUS context rewriting (Mach-port receiver avoided)
- [x] Guard-page execution on Linux/amd64, Linux/arm64, and Darwin/arm64.
  All six native targets support explicit bounds; Darwin/amd64 and Windows use
  explicit bounds rather than signal-backed guard pages.
- [x] Verify json-as serialize/deserialize in explicit and guard modes and SQLite's
  recursive-CTE aggregate workload against committed goldens on Darwin/arm64
- [x] Reference globals, heterogeneous indexed table operations, and nonzero-table
  `call_indirect`, with native Linux/arm64 and Darwin/arm64 CI gates
- [x] Explicit-bounds `CoreFeaturesV3`, including tails, typed references, WasmGC,
  exception handling, multi-memory, memory64, and table64, with a zero-gap complete
  official suite under Linux/arm64 QEMU and native Linux/Darwin arm64 CI gates

## Current work (near-term)

This roadmap is authoritative for current optimization priorities. The lists
keep completed items for context; look for 🚧 and [ ] to find work that remains.
Current tracks:

**WebAssembly 3.0** (primary conformance complete; product hardening continues)
- [x] Pin and execute the complete official `WebAssembly/spec` `wg-3.0` corpus.
  Linux/amd64 native and Linux/arm64 QEMU explicit-bounds `CoreFeaturesV3` runs
  pass all 2,226 modules and 58,038 assertions with zero failures, skips, or gap
  categories; native Linux/Darwin arm64 runs are now required in CI.
- [x] Complete mandatory extended constants, relaxed SIMD, tails, typed function
  references, GC, exception handling, multi-memory, memory64, and table64 on the
  primary product. Tail calls, typed function references, multi-memory, memory64,
  and table64 now default on for complete backends. WasmGC and exception handling
  joined them when the complete Core 3 release became the default.
- [x] Add exact linux/amd64 and Linux/Darwin arm64 WasmGC roots across local
  direct/indirect/reference calls, recursion, bounded host re-entry,
  mutable/shared GC globals, local/shared collector-reference tables, EH payload
  records, local starts, and same-Runtime cross-instance calls. One-/two-word and
  flat masks feed variable-size exact root vectors; locals dead at every collecting
  site are compacted from the plan. Codec version 3
  validates the native maps and the required native-GC ABI version.
- [x] Add snapshot version 1 stable-ID heap graphs for objects reachable from owned
  local GC globals and one or more heterogeneous local collector-reference tables,
  preserving cycles, sharing, growth state, strict structural subtype validation,
  malformed-input rejection, and native ARM64 execution without serializing compact
  handles.
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
  including the wrapper save-area stack adjustment. Descriptor-resolved
  `return_call_ref` performs bounded discarded-frame transfer to local internal,
  host-wrapper, and retained same-Runtime cross-instance targets, including mutable
  and imported funcref-table loads. GC-bearing typed tails require exact equal
  collector-domain identities; host and foreign-domain GC transfers remain fail-closed.
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
- [x] **Cross-module canonical GC type identity:** Runtime domains canonicalize
  recursive structural types and give every instance an immutable local/domain type
  map. Structurally equivalent producer/consumer modules with reordered or additional
  local types share one collector without treating raw module-local indexes as identity;
  collector descriptors append safely under the domain lock, codec-loaded modules and
  checked host tokens use the same mapping, and incompatible layouts/configurations
  remain fail-closed.
- [x] **Host-held GC results:** generic struct/array results use a 64-slot inline
  fast path plus reusable dynamic overflow storage, atomically roll back partial
  multi-result egress, retain exact Runtime/store ownership after producer close,
  and reject stale/cross-producer release.
- [x] **Host GC token ingress:** non-null `GCRef` arguments re-enter only the exact
  collector domain after structural subtype validation, use up to 64 reusable checked
  roots, survive concurrent release after staging, and reject stale/foreign tokens.
- [x] **Runtime-owned GC host calls:** `Runtime.NewGCHostFuncRef` binds one explicit
  host function to an exact canonical Runtime collector domain on first import.
  Direct/indirect/reference calls and discarded-frame `return_call_ref` translate
  compact arguments to temporary opaque tokens, validate typed token results, and
  retain bounded argument/result roots across parked host execution. Ordinary host
  owners and direct foreign Runtime domains continue to reject.
- [x] **Bounds-mode parity:** linux/amd64 explicit and signal-backed Core 3 pass
  2,226 modules and 58,038 assertions with zero failures, skips, or gaps. ARM64
  signal builds now admit exception handling, multi-memory, and memory64: memory 0
  uses native guard faults while indexed memories and memory64 retain carry-safe
  explicit checks. Native Linux/ARM64 and Darwin/ARM64 `spec3-signals` are mandatory
  CI cells alongside their explicit-bounds cells.
- [x] **Versioned native GC metadata and checked scalar hot paths:** collectors
  publish one stable ABI version 1 view whose handle/heap pointers, lengths,
  subtype-interval pointer/count, and generation refresh after every relevant
  mutation; each instance adds an immutable local-to-domain type view at basedata
  offset 280. AMD64 scalar struct/array get/set lowering validates ABI version,
  handle kind/range, space range, object extent, canonical type, and array index
  before direct access. Non-final defined struct/array `ref.cast` and `ref.test`
  now use canonical DFS interval containment after the same checked resolution,
  while exact casts use canonical ID equality. Reference/v128 loads, bulk, and
  barrier-requiring operations remain helper-bound. Focused non-final cast/test
  loops measure 3.54–3.66 ns/op, 0 B/op, and 0 allocs/op, with zero synchronous
  helper calls under the since-removed `wago_gcstats` diagnostics.
- [x] **Bounded structured WasmGC facts (#314):** AMD64 carries compact
  nullability/heap/exact-type/identity/freshness/generation/pointer-free/array-length
  facts through Valent stack values and locals, intersects them at structured joins
  including exception catches, preserves hidden operand roots across `try_table`,
  treats abstract `any`/`eq` classes as upper bounds, and retains only backedge-safe
  loop facts and immutable forwarding. It folds proven tests/casts and constructor
  lengths while keeping raw resolved addresses in a separately safepoint-invalidated
  one-entry certificate. A bounded result-local cache now
  eliminates repeated dynamic `array.len` and immutable `struct.get`, immutable values
  survive unrelated mutable effects, constructor-known constant indexes use a compact
  get/set sequence, and validated subtype forests use packed constant-time intervals
  after a shallow parent fast path. Bounded dead-constructor proofs now cover nested
  struct/fixed-array trees; pointer-free uniform/data and default-initialized drops preserve the real
  bounded-heap allocation side effect while omitting unreachable payload population.
  Reference-valued uniform/element constructors retain their full edge/card path.
  Broad scalar replacement remains rejected by measured
  frame growth rather than introducing SSA or a second IR. Memory32 loop prechecks
  canonicalize i32 bases; memory64 and candidate native-root-plan functions do not
  version until carry-safe elision and explicit liveness-stream remapping exist.
- [x] **Explicit late GC barrier states (#315):** reference stores select
  `NoBarrier`, `YoungParent`, `KnownOldChild`, `ExistingCard`, `CardMark`, or
  `SlowBarrier` after structured facts. Null/i31 scalar stores and guarded null/i31
  `array.fill` omit barrier work; generation-only `YoungParent`/`KnownOldChild`
  selection remains disabled until relocation, marking, and remembered-set proofs are
  separated. Unknown profile/generation, metadata growth, foreign/malformed refs, and
  Tiny incremental shading retain checked native/helper paths. Throughput `array.init_elem` validates the complete source once, publishes
  one post-write destination range, and avoids duplicate release-path ownership checks;
  Tiny bulk publication is chunked, and diagnostic telemetry counts every checked
  barrier state separately. Copy/fill/init retain exact overlap and trap atomicity, with
  permanent nursery/remembered-old/unremembered-old/large/Tiny barrier benchmarks.
- [x] **Bounded foreign-Runtime GC graph transfer:** `target.CloneGCRefFrom(source,
  ref)` selects explicit transactional graph cloning rather than sharing compact
  handles. It preserves cycles and internal sharing under 1,024-object, 65,536-value,
  and 1 MiB payload bounds, remaps structurally equivalent target types, roots both
  phases exactly, and rejects non-null funcref/externref payloads. The clone receives
  new target identity; direct foreign-domain reference calls remain fail-closed.
- [x] **Bounded Tiny incremental work (#319):** marking retains one stable
  handle/index cursor and caps each object-mark `Step` at 64 ranges, 256 entries,
  256 references, and 1,024 payload bytes. Seven-bit epochs make cycle start O(1).
  Transient roots use an atomic allocation-free 1,024-reference direct walk;
  globals/tables resume in 256-slot chunks. Sweep caps 64 handles, 256 blocks,
  and 4,096 poison bytes, while allocation uses swept spans without draining the
  cycle. Physical allocation debt buys bounded Steps and near-exhaustion assists
  stop at 32. Sweep barriers pause the cursor and enqueue ordinary bounded mark
  work; measured SATB was rejected because its required scalar/bulk deletion
  barriers increased product complexity without solving a remaining latency gap.
  `wago_tiny_nonincremental` provides the measured smallest synchronous policy.

**Engine & performance** (no-ir-plan P1–P7, measured against P1's stats)
<!-- roadmap:P1 status=done -->
- [x] **P1 — `CodegenStats` + explain mode**: per-function counters,
  `WAGO_EXPLAIN`, golden-disassembly harness, `WAGO_DEBUG_MODGLOBALS`, and
  `WAGO_PIN_GLOBAL_K` are implemented on amd64 and arm64.
<!-- roadmap:P2 status=partial -->
- 🚧 **P2 — cheap railshot wins**: the const-fold pack, same-operand integer
  identities, direct commutative self-updates, generalized low-32 i64 masks,
  alias-aware pending loads, and pure-tree `drop` are landed. Broader narrow-load
  mask elision remains measurement-gated.
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
<!-- roadmap:P7 status=partial -->
- 🚧 **P7 — compile path**: post-#96 profiling rejected validator/codegen
  entanglement as the first move. The existing requirements scan now also
  produces module footprint facts, passive-segment state bounds, and atomic
  wait-helper selection, indexed-function ref-test/cast requirements, and the
  ARM64 GC ref-test helper obligation, removing four later whole-body walks.
  Identical-commit full-compile A/Bs on darwin/arm64 show 7.70–9.90% for the
  first summary fusion and a further 9.99–13.94% for the ref-operation fusion
  on ruby, esbuild, sqlite3, and json-as, with allocation traffic and peak RSS
  neutral. Fusing support admission and backend hints remains premise-gated on
  preserving validation/error order and per-architecture code identity.

**Runtime & product** (no-ir-plan P8 — parallel track, feature value)
- [x] **Synchronous host-import results** — returning host imports use the no-cgo
  re-entry protocol; `v128` host params/results use the same two-slot public ABI.
- [x] Interruption / cancellation: Linux/amd64 and Linux/arm64 use
  thread-directed real-time signals plus validated `ucontext` rewriting, with
  zero interruption instrumentation in generated Wasm. Other targets retain
  function-entry and loop-header polls. Both Linux architectures return
  `context.Canceled`/`DeadlineExceeded`, and active-instance close uses the same
  trap. Standard Go releases the P at the foreign boundary. TinyGo release
  builds use the safe `tasks` scheduler and reject cancelable native calls.
- [x] Wasm-level trap source frames: generated cold edges report the logical
  function (including inlined callees) and an exact Wasm PC when a function has
  one site for that trap class. Shared multi-site stubs still report the
  function without guessing a PC. Full caller-chain unwind metadata remains a
  follow-up.
- [x] WebAssembly 2.0 product closeout: `.wago` codec version 3 persists structural
  reference globals, indexed typed tables/exports/elements, exact local/imported
  table/memory-limit forms, indexed memory imports/exports, and required-feature
  bits without serializing live runtime identity.
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
- [x] Runtime-only standalone commands: `wago compile` precompiles the reviewed
  module/plugin graph into a native `.wago` artifact and embeds it in a
  `wago_precompiled` loader, retaining no Railshot source compiler. `--tinygo`
  selects TinyGo for the final link and strips the resulting native binary.
  Platform-sensitive codegen keeps both link paths native-target-only.

## Verification and quality

- [ ] Differential oracle: fuzz modules, compare results/traps against C++ WARP (the
  off-path `src/core/compiler/ir` package is reserved as this oracle)
- [ ] Byte-for-byte codegen diffing against WARP for shared inputs
- [ ] Golden disassembly regression net (grows one golden per optimization from P1 on)

## Larger feature areas

- [x] SIMD (`v128`) — complete for the documented AMD64 SSE2 baseline with optional modern tiers: every decoded core SIMD opcode and deterministic relaxed SIMD opcode through 0xfd 275 is frontend-admitted, validator-admitted, and lowered by railshot; reserved proposal-table holes are invalid-decode tests. Public `[16]byte` (`wago.V128`) plumbing covers locals, params/results, control flow, globals, cross-instance imports, and host imports/results. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). Keep AVX2/FMA/VNNI optimizations behind future CPU gates.
- [x] **Threads & atomics** — bounded experimental
  product on Linux/macOS amd64/arm64 with explicit bounds and one memory32;
  shared memory must be an exact-max import, while unshared memory can be local
  or imported. It includes true distinct-instance native overlap, the full classic
  atomic matrix, and bounded wait/notify. Broader shared-everything threads, growth,
  memory64/multi-memory, signal bounds, mutable global imports, GC/EH, and
  snapshots remain deliberately outside this product. Same-instance entry is
  accepted but serialized around the instance's reusable invocation state.
- [x] Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`) —
  complete for the Core 3 linux/amd64 explicit-bounds product, including local,
  host, cross-instance, indirect, typed-reference, trap, and validation paths.
  Broader platform and bounds-mode parity is tracked above.
- [x] Basic extended constant expressions: integer add/sub/mul, prior immutable
  globals, active offsets, strict validation, and codec version 3 persistence.
- [x] Typed function references — recursive structural typing, typed tables,
  elements and globals, `call_ref`, casts/tests, null branches, linking, ownership,
  codec metadata, and official invalid/unlinkable behavior are complete for the
  primary Core 3 product. A compact exact per-function type-ID directory preserves
  dynamic `ref.test` identity after GC-struct and other storage loads without growing
  canonical descriptors, including eqref-transported direct/environment-first closures.
  Indexed function-reference lowering is restricted to function heap types, so a concrete
  closure struct still casts through its declared non-final struct supertype. Matching
  `ref.null eq`/`i31`/`struct`/`array` global constant initializers are also admitted.
- [x] Multi-memory, memory64, and table64 — mandatory Core 3 execution, metadata,
  codec, linking, traps, and official tests are complete on linux/amd64 explicit
  and signal-backed bounds. Linux/Darwin arm64 explicit-bounds execution is
  implemented; the full official suite passes under Linux/arm64 QEMU and native
  Linux/Darwin arm64 runs are required in CI. Broader arm64 guard-page parity
  remains a platform-qualification item.
- [x] Reference-types product completion: signatures, locals, control,
  local/imported/shared globals, host ABI, explicit host funcref ownership/egress,
  typed 8-byte externref tables/elements, every `table.*` operation, multiple
  local/imported tables, exact exports/re-exports, codec version 3 structural metadata,
  snapshot isolation, complete inspection, cross-link teardown, and the
  zero-skip Release 2 execution corpus are done.
- [x] Native Linux, macOS, and Windows runtime paths on amd64 and arm64, with
  mandatory native CI and release assets for all six targets.
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals for now

- An interpreter tier (supported modules execute as native code)
- **An SSA / IR execution tier** — decided against 2026-07-03; Railshot is the
  only backend, and the ceiling is attacked incrementally instead.
- Re-implementing WARP's linker/disassembler/fuzzer (WARP remains the external
  reference)

## Historical milestones

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
argument/result ownership. A later default-policy pass promoted the lower-risk
tail, typed-reference, and indexed/wide memory/table families on complete
backends while retaining GC and exceptions as explicit Core 3 opt-ins. That
staged policy was later superseded when the complete Core 3 release became the
default on complete backends.

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
Tiny stress. Direct tail calls discard each caller frame and retain no callsite roots. Codec version 3
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
activation. Codec version 3 persists and validates the new callsite shape. Throughput
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

## August 2026 wasm3 audit hardening

- [x] Synchronize mutable generic-GC globals at allocating array-helper safepoints
  and support non-defaultable non-null reference `array.new_elem` construction.
- [x] Separate Linux SIGSEGV/SIGBUS predecessor chaining, publish handlers before
  installation, roll back partial installs, and commit reservation-relative
  64-KiB Wasm pages on guard growth faults.
- [x] Make cancellation cleanup panic-safe and bound native signal retries while
  guest execution is parked in host code.
- [x] Execute the complete memory32 65,536-page boundary with u64 byte-size caches
  while preserving u32 page counts; reject oversized host memory declarations.
- [x] Remove the duplicate required-feature body scan, make reference-array root
  composition allocation-free with reusable scratch, switch early Throughput
  growth to geometric capacity, and release the duplicate Go-heap JIT code copy
  after RX mapping.
- [x] Historical GC telemetry experiment (since removed): bounded pause
  histograms, additive phase timing, exact trace/root/card/promotion/path counters,
  managed-memory domains, JSONL A/B reports, code-neutral JIT byte attribution,
  and hot-versus-sparse static-site benchmarks. Ordinary builds retain the
  uninstrumented collector path and only one 4-KiB stripped-file alignment step.
- [x] Remove linear Throughput old-space allocation: constant-time class mapping
  feeds one arena-backed augmented AVL free-span index with logarithmic fit,
  insertion, exact coalescing, top-bump reclamation, exact fragmentation
  summaries, lazy post-full-GC indexing, size-grouped promotion destinations,
  randomized interval-oracle/fuzz coverage, and warmed allocation-free churn.
- [x] Make Throughput minor collection card-driven: measured 128-byte payload
  cards retain linked disjoint ranges, persistent globals/tables use stable-index
  bitmaps instead of a Go map, bulk barriers avoid a mutator rescan, generated
  same-card stores stay native, and metadata-growth failures take one shared safe
  whole-object/full-root fallback until evacuation. Removed/coalesced card slots
  are reused at the peak high-water mark, and helper hits swap non-head intervals
  into the stable native head slot; repeated non-head writes improve 16.9% in the
  interleaved control. The 256K two-write fixture falls from 262,144 to 64 scanned
  slots while dense work remains within 3% of baseline.
- [x] Historical wide-root phase: retain the one-word <=64-root path, add
  two-word <=128-root masks and a bounded flat arena through 1,024 roots, admit
  exact local starts, omit independently proven non-collecting functions, share
  repeated immutable offset maps, and expose fail-closed diagnostics. This was
  not a permanent semantic ceiling; the following per-site-liveness work removes
  it. The one-root 16K-instruction compile benchmark improves 3.2% while
  temporary bytes fall 18.6%; dense safepoint lookup remains about 1.66 ns, zero
  allocation.
- [x] Base large-frame admission on per-site liveness: track the configured local
  population, compact locals dead at every collection point, and emit variable-size
  exact root vectors. Function parameters plus declared locals default to 65,535;
  lower configured admission limits remain available, and the native 256 KiB stack
  fence remains independent.
- [x] Add bounded Throughput survivor aging: Eden feeds two bump-copy semispaces,
  handle-owned age bits retain the 20-byte native entry, medium-lived objects
  tenure after measured survival, and large young objects age in place. Exact
  cards persist only while young edges remain, movement is transactionally
  preflighted, and occupancy/old-pressure/full-GC plus optional pause targets
  adapt the threshold from one through three minors. The one-minor workload
  eliminates 32 promoted bytes/object and improves its median about 19%; the
  zero-survival path remains allocation-free and within 1% of immediate promotion.
- [x] Extend transactional AMD64 nursery allocation through #311: the retained
  32-handle batch now exposes contiguous runs and optional bounded 4-KiB array chunks;
  statically sized final numeric/vector/packed and nullable abstract-reference
  arrays up to 256 object bytes use checked native default, uniform, and fixed
  constructors. Dynamic, large, data/element-segment, and unsupported reference
  shapes remain exact rooted helpers after measurements rejected broader native
  admission. ABI version 1 keeps the established direct struct bump, delays generic
  array refill until nine slow constructors, cancels unused handles/chunks on every
  Go allocation, trap, collection, epoch change, and close, and reconciles exact GC
  globals after successful helper-free invocation sequences.
- [x] Keep default and guard-tag runtime/Wago suites green, including explicit-
  bounds snapshot fixtures and cross-architecture compile gates.
- [x] Admit large GC struct helpers through a module-derived synchronous host
  frame extension: 404-slot reference constructors retain exact initializer
  roots, AMD64/ARM64 share the u16 check, codec reload recomputes capacity, and
  ordinary modules retain the 64-slot inline frame and unchanged `Compiled` size.
