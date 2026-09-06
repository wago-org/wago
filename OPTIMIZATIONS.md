# Wago optimization roadmap

This is a technical record for maintainers. It tracks changes that improve Wago
without losing its core properties: fast compilation, no cgo, small footprint,
and one direct backend.

## Start here

If you are new to the project, read [Architecture](ARCHITECTURE.md) first. Then
use this file as follows:

- **Current choices and priorities:** read [Remaining roadmap](#remaining-roadmap-priority-ordered).
- **Why Wago has one execution backend:** read
  [Architecture decision: no IR](#architecture-decision-no-ir-revised-2026-07-03).
- **Measured results for shipped work:** read [Landed work and measurements](#landed-work-and-measurements-updated-2026-08-30).

The dated sections preserve measurement context and historical decisions. A result
applies only to the hardware, configuration, and revision stated beside it.

Two complementary approaches guide the work:

1. Make the single-pass backend smarter with better choices inside Railshot.
2. Port only useful ideas from [WARP](https://github.com/wago-org/warp), the C++
   reference engine whose backend design Wago ports. WARP is a reference, not a
   target to clone.

The architecture decision is **no IR on an execution path**. Railshot is the only
backend. The `src/core/compiler/ir` SSA package remains an off-path research and
debug tool, not a planned tier. Wago addresses the benefits often associated with
SSA through small, measured changes instead.

Legend: effort S/M/L · value ⬜ low · 🟦 medium · 🟩 high · ⭐ very high.

---

## Landed work and measurements (updated 2026-08-30)

The entries below are an evidence log. They describe the change, its measured
benefit or cost, and rejected alternatives. They are not a list of promises.

**Benchmark-audit frontend wins (2026-08-30).** Indexed multi-memory memargs in
allocation-free bytecode walks now decode directly into `InstructionImmediate`
instead of constructing the AST pointer form. A 10,000-load mixed-width fixture
improves **287.6→211.7 µs/op** (-26.4%), **42,183→1,915 B/op**, and
**10,001→1 alloc/op**. Large function-import modules also avoid quadratic
`ImportedFuncCount`, `FuncSignature`, and `FuncTypeIndex` rescans: GC-boundary,
synchronous-host-slot, and imported-signature prepasses each range the import
section once, while frontend diagnostics format only on failure. Ten-sample
`benchstat` results improve the 10,000-import compile watchpoint
**217.278→5.361 ms** (-97.53%), **6,364,072→5,486,008 B/op**, and
**79,818→40,071 allocs/op**. Ten interleaved sqlite3, ruby, and esbuild corpus
samples are statistically unchanged with a -0.43% geomean and identical
allocation counts. The stripped manager size is unchanged; runtime-standard and
runtime-minimal each grow 4,096 bytes. Stale resource-limit and survivor-policy
benchmark fixtures were repaired.
Adjacent import-name reuse and an inline mixed-memory width word were measured and
rejected.

**Commutative self-updates and low-32 masks (2026-08-29).** AMD64 now
accumulates every safe non-fixed `x = f(y) op x` form directly in `x` instead of
spilling the old destination, including the first site in a function. The direct
lowering avoids the generic relocation path and is default-on with
`WAGO_AMD64_NO_COMMUTE_SELF_UPDATE=1` as the A/B oracle. A same-process seven-row
corpus watchpoint improves **3.55% geomean**: scalar BLAKE3 improves
**718.4→703.3 µs** (-2.1%), SIMD BLAKE3 **628.2→575.8 µs** (-8.3%), and the
open-coded multiply-high loop **2.239→1.995 µs** (-10.9%); the worst row is
spectral norm at +0.14%. Across the complete compile corpus, emitted code falls
**69,708,236→69,675,415 bytes** (-32,821) and allocator spills fall
**3,817→1,259** (-67.0%), with 2,556 retained sites. The nearby #438 mask
experiment also generalizes `i64.and` to use a zero-extending 32-bit immediate
for every mask through `0xffffffff`, not only the full low word. Runtime is flat
in dependent loops, while 7,483 corpus sites remove 4,800 native bytes beyond
the old exact-mask rule. `BenchmarkExecCommuteSelfUpdate`,
`BenchmarkCompileCommuteSelfUpdate`, and `BenchmarkAMD64Low32MaskInstruction`
are the permanent A/B watchpoints. Default-off float-compare fusion, vector
sinking, loop prechecks, tee-spill reuse, call next-use, and affine LEA were
remeasured and removed; BMI2 RORX remains rejected and opt-in.

**Scalar float mask and copysign lowering (2026-08-29).** AMD64 implicit
abs/neg/copysign masks now use the existing RIP-relative constant pool instead of
a GPR rebuild. Focused dependent loops improve `f64.abs` **0.571→0.447 ns/op**
(-21.8%) and `f64.neg` **0.564→0.449** (-20.4%). Copysign now uses one mask and
three-operand VEX XOR/AND identities while borrowing pinned operands; the full
change improves **1.179→0.898 ns/op** (-23.8%) and reduces the fixture from 157
to 136 bytes. A min/max branch-layout rewrite was rejected after regressing every
measured ordered path, and BMI2 RORX remains experimental after matching the
baseline rotate while adding two bytes.

**Signed unit-divisor lowering (2026-08-29).** AMD64 now lowers constant
`div_s 1` to identity, `rem_s ±1` to zero, and `div_s -1` to an exact `INT_MIN`
overflow check plus `neg`, avoiding IDIV while preserving the Wasm trap. Focused
dependent guest loops on the Ryzen 7 8845HS improve `i64.div_s 1`
**3.462→0.443 ns/op** (-87.2%), `i64.rem_s 1` **3.074→0.439** (-85.7%),
`i64.rem_s -1` **0.657→0.446** (-32.1%), and `i64.div_s -1`
**3.650→0.444** (-87.8%), all at 0 B/op and 0 allocs/op. Current constant versus
dynamic fixture code sizes are 101/219, 103/172, and 158/219 bytes for divide by
1, remainder by 1, and divide by -1. A three-operand immediate-IMUL remainder
reconstruction was measured and rejected after slowing `i64.rem_u 3` about 3.6%.

**High-pressure associative Valent covers (2026-08-10).** AMD64 deferred nodes
now retain their Sethi--Ullman register-need label at construction. The
associative `add`/`and`/`or`/`xor` cover uses the existing height-six Valent
bound as fuel instead of an unrelated eight-leaf array, so every representable
tree (up to 64 leaves) can be considered without allocation. Requested
destination registers are admitted only for need-four-or-greater trees; a
single old-destination input becomes the accumulator seed. Repeated borrowed
local/global reads preserve the old destination once in a pinned scratch
register and retarget the remaining bounded subtrees to that copy; aliases with
owned-register state retain the ordinary allocator path. The combined current-
corpus result is 620 explicit-mode destination covers, **4,529 fewer native
bytes**, **166 fewer spills**, and six fewer reloads. Guard mode selects 552
destination covers, removes 652 bytes, 14 spills, and two reloads. Hits come
from QuickJS/script, SQLite, and Ruby; all runnable corpus modules retain the
same codegen statistics. A broader need-three policy was rejected after it
regressed the six affected execution rows by 0.35% geomean. Integer
multiplication was also rejected after producing zero new covers across all 64
corpus modules. Twelve interleaved pinned samples put SQLite/Ruby backend and
full compile at a statistically flat 0.9994x geomean for the repeated-alias
extension; forced-worker Ruby codegen is likewise flat at 1.0012x, with
allocation volume unchanged.
The full AMD64 backend, vet, and explicit/guard/wazero corpus gates pass.

**Direct module-fact scalar scanning (2026-08-02).** `AnalyzeModuleFacts`
needs only `memory.grow`, `table.grow`, and `ref.func`, but previously built full
instruction metadata for every unrelated scalar operation. It now consumes
immediate-free operations, constants, branches, calls, and local/global/table
indices directly while preserving strict immediate validation; uncommon and
fact-bearing forms retain the general classifier. On linux/amd64 (Ryzen 7
8845HS, GOMAXPROCS=1, one pinned CPU), the 96 KiB scalar facts watchpoint improves
from **1.073 ms to 209.0 µs median** (**−80.5%**), and the real 8.47 MiB esbuild
body-facts stage improves **77.11→30.14 ms** (**−60.9%**), both with unchanged
allocations. Across two surrounding seven-sample full esbuild runs, the pooled
post-change median is **816.5 ms** versus the interposed **846.5 ms** baseline
(**−3.5%**, allocations unchanged) despite host-frequency noise. A same-session
plugin-complete stripped TinyGo A/B costs **352 bytes**
(**1,736,916→1,737,268 bytes**, +0.020%).

**Fast bytecode requirement summaries (2026-08-02).** Compiled-artifact
feature discovery and other summary walkers used the full immediate classifier
for every instruction, including immediate-free scalar ALU operations and common
constants/local/global accesses. The wasm walker now exposes the dense
immediate-free opcode table and keeps common scalar immediates on the exported
reader; feature discovery consumes those forms directly while preserving exact
LEB validation and proposal classification. On linux/amd64 (Ryzen 7 8845HS,
GOMAXPROCS=1, one pinned CPU, 10–12 one-second/one-iteration samples), a 96 KiB
scalar requirement scan improves from a **1.198 ms to 238.8 µs median**
(**−80.1%**, 0 B/op), `AnalyzeModuleFacts` on the same stream improves
**1.154→1.073 ms** (**−7.0%**), and full esbuild decode+validate+compile improves
**899.3→776.3 ms** (**−13.7%**) with unchanged allocation counts. The common
walker fast paths cost 1,008 bytes in a same-session plugin-complete stripped
TinyGo A/B build (**1,735,844→1,736,852 bytes**, +0.058%). Differential
feature-family tests keep
tail calls, typed references, exception handling, sign extension, SIMD, GC, and
segment lifecycle detection exact.

**Dense `br_table` compile scratch (2026-08-02).** Duplicate-heavy jump tables
previously allocated a `map[uint32]int` per instruction to deduplicate branch
stubs. Branch labels are bounded control-stack depths, so amd64 and ARM64 now
reuse one dense position slice per compiler worker and initialize only depths
used by the current table. On linux/amd64 (Ryzen 7 8845HS, GOMAXPROCS=1, one
pinned CPU), the new 256-entry mixed-label watchpoint improves from a
**505.9 to 458.5 µs/op median** (**−9.4%**, ten one-second samples); existing
small duplicate/mixed watchpoints improve **75.41→72.68 µs** (**−3.6%**) and
**118.45→113.13 µs** (**−4.5%**). The real esbuild codegen benchmark removes
**8,335,959 B/op** (**80,910,800→72,574,841, −10.3%**) and **15,713 allocs/op**
(**126,583→110,870, −12.4%**); median latency is within noise at
414.4→412.6 ms. This focused compile-memory/synthetic-speed win costs 160 bytes
in the plugin-complete stripped TinyGo release (1,734,452→1,734,612), leaving the
combined context-binding plus jump-table changes 16 bytes above the prior
1,734,596-byte release.

**Direct native-context rebinding (2026-08-02).** Every public native entry
restores ten stable context pointers into basedata. The prior path decoded those
bytes into an `InstanceContext` and then performed ten separate slice-backed
setter operations. The native platforms now decode potentially unaligned source
bytes safely but store directly into the already bounds-checked, naturally aligned
off-heap basedata image. On linux/amd64 (Ryzen 7 8845HS, GOMAXPROCS=1, one pinned
CPU, 15 one-second samples), `BenchmarkBindInstanceContextBytes` improves from a
**11.24 to 2.987 ns/op median** (**−73.4%**). End-to-end prepared `tiny.add`
invocation improves **47.87→39.24 ns/op** (**−18.0%**), remaining at 0 B/op and
0 allocs/op. The plugin-complete stripped TinyGo release also falls
**1,734,596→1,734,452 bytes** (**−144 bytes**). Unaligned-source, exact-field,
race, full-suite, and Linux/Darwin ARM64 cross-build coverage preserve the
112-byte context ABI and leave GC-domain/tail metadata untouched.

**Mutable imported-funcref proper tails (2026-08-01).** Descriptor-driven
`return_call_ref` now tail-transfers through a mutable imported funcref table while
preserving exact same-Runtime GC-domain ownership and zero-allocation invocation.
On linux/amd64 (Ryzen 7 8845HS, five samples), the collection-capable public
`Invoke` benchmark measures **474.7–481.6 ns/op**, 0 B/op, and 0 allocs/op. Existing
direct cross-instance `return_call` watchpoints remain **89.52–94.63 ns/op** across
integer, mixed-float, and float-parameter/integer-result shapes, also allocation-free.
The ownership check itself adds no collector metadata or descriptor words. Stable
domain identity and descriptor-tail scratch first extended the off-heap native
instance-context buffer from 72 to 104 bytes. The checked-object work below adds the
per-instance view pointer at byte 104, making the buffer 112 bytes (**+40 bytes from
the original context**), and grows basedata from 272 to 288 bytes for its active slot.
ARM64 uses the same bounded descriptor semantics but still has the existing
spill/reload staging optimization headroom.

**Checked direct WasmGC object access (2026-08-01).** The measured prototype is now
an AMD64 production path for final scalar struct/array get/set. Collector ABI v2
publishes one stable 128-byte view preserving the v1 handle/space/generation prefix
and appending object-card pointer/count metadata; allocation, collection, and card
backing changes republish it in place. Each collector-backed instance retains one 32-byte
native prefix with the immutable local-to-domain type map and publishes it at basedata
offset 280. Generated accesses validate both ABI versions, handle tag/range, space,
heap/object extent, exact canonical type, and array index. Numeric stores are
pointer-free; non-final, reference, `v128`, bulk, and barrier-requiring paths stay on
helpers. On linux/amd64 (Ryzen 7 8845HS, five samples), end-to-end struct set/get is
**227.9–229.4 ns/op**, struct get **218.2–219.9 ns/op**, and array set/get
**265.2–265.6 ns/op**, all 0 B/op and 0 allocs/op. The underlying checked metadata
walk remains **4.05–4.71 ns** versus **20.26–29.82 ns** through collector methods.
The stripped TinyGo release grows 1,985,784→1,999,256 bytes for this production
slice (**+13,472, +0.68%**). The subsequent bounded foreign-clone API brings the
combined candidate to 2,000,232 bytes (**+976 bytes further**).

**V8/Cranelift/Binaryen WasmGC optimizer research (2026-08-02).** Current V8
Turboshaft propagates path-sensitive exact/non-null facts, removes redundant
casts/null checks, forwards struct fields and immutable array lengths, marks fresh
allocations non-aliasing, and recursively removes dead allocation/store trees.
Current Cranelift specializes static reference classes and its copying collector
emits overflow-safe inline bump allocation with one cold collection/growth slow
path. A Binaryen type-flow oracle on Dew removed exactly 1,024 dead
`array.new_fixed` + `struct.new` trees: Wago `hostsync` fell **8,188→6,140**,
fresh median **0.94→0.63 ms**, host allocation **924,536→524,512 B/op**, and
native code **7,479,004→7,112,416 bytes**. Heap2Local alone was neutral-to-slower
and expanded the frame **24,808→82,152 bytes**, so broad scalar replacement is
rejected as the next step. Recursive dead-constructor elimination is now shipped
on AMD64: a bounded postfix stack-depth proof follows values through nested
`struct.new`/`array.new_fixed` containers, while direct constructor/drop pairs are
removed at the constructor. Pointer-free dynamic `array.new`, default-initialized `array.new_default`, and
pointer-free `array.new_data` use allocation-reservation helpers 36-38. They preserve current bounded-heap exhaustion,
collection, handle publication, allocation counters, size/capacity, and segment-range
traps while omitting payload population for the unreachable result. Reference-valued
uniform and element-segment constructors retain the full constructor path because
omitting their edges/cards could change later minor-collection retention and capacity. Non-trapping valent
trees disappear completely; any deferred trap forces original bottom-to-top
evaluation. These helpers retain real GC safepoints. The differential control remains
`WAGO_AMD64_NO_DEAD_GC_NEW=1`. After the request-changes correction, permanent
fixtures reduce constructor-family code from **142→128 bytes** for dynamic default,
**178→164** for numeric uniform, and **214→200** for data reservation; reference
element construction remains **214→214**. A nested default wrapper falls
**312→128 bytes**. Earlier 64/82/100-byte figures used a
size-only preflight that was not equivalent on an occupied bounded heap. On Dew it fires exactly
**2,048** times, cuts `hostsync` **8,188→6,140**, generated code
**7,479,004→7,104,256 bytes**, fresh median about **0.94→0.62-0.67 ms**, and host
allocation **924,536→524,512 B/op**. The stripped plugin-complete TinyGo binary is
**1,776,700 bytes**, +2,512 bytes over the preceding 1,774,188-byte build. The next
order is structured exact/non-null facts and bounded GC load forwarding, then native
bump allocation for constructors that remain live.

**Structured WasmGC reference facts (2026-08-10, #314; retired 2026-09-03).** AMD64 experimented with a
backend-neutral bounded two-word fact for compact GC references: nullability,
abstract heap class or exact canonical type, bounded semantic identity,
fresh/publication state, generation, pointer-free layout, and optional constant
array length. Facts move with Valent stack storage and locals, intersect at structured
joins, and preserve loop-invariant locals using the loop scan's existing modified-local
set. `select`, block parameters/results, `br_on_null`, `br_on_cast`, casts, tests, and
successful dereferences retain or refine the safe intersection. Structured facts may
survive collection because they contain only compact semantic identity; the separate
one-entry resolved raw address certificate is still invalidated at every safepoint,
call, allocation, difficult merge, loop edge, and unknown effect. No SSA, second
optimizer IR, whole-function alias analysis, or raw-pointer retention was added.
Constructor lengths now replace `array.len` with constants; null/exact tests and casts
fold; constant fresh `struct.set` values can forward to a same-field get; and facts
remain bounded to local tables plus one-entry load windows. The experiment and
its public switches were later retired after broad measurement found neutral
execution but materially worse compile resources. Required root tracking and the
independent one-entry resolved-address cache remain. The earlier exact-only Dew result
(**7,158** cast eliminations and **50,101→42,943** `gcnative` sites) remains historical
baseline evidence; the expanded fact set requires a fresh interleaved qualification
before making an additional workload-speed claim.

**Structured-fact adversarial hardening (2026-08-11).** `try_table` and synthetic
inline frames now preserve hidden operand-root shape, and catch edges participate in
local-fact intersections. Abstract `any`/`eq` classes are upper bounds, not exact
runtime families; a complete class/target truth table keeps narrowing tests and casts
dynamic. Ordinary loop headers discard mutable field forwarding, publish surviving
fresh locals, and retain immutable cached results only across invariant locals. Loop
versioning is memory32-only, zero-extends host-produced i32 bases before precheck
arithmetic. The later loop-versioning experiment was removed after its broad
execution benefit failed to justify duplicated code generation and compile-resource
cost. Facts and load-forwarding subprocess oracles continue to compare exact results
and trap codes under both switch states.

**Executed WasmGC helper counters (2026-08-02).** The diagnostic
`wago_gcstats` build tag exposes `Instance.SetGCHelperStatsTracking(true)` and
`Instance.GCHelperStats()`, separating total, allocation, struct/array mutation,
reference-mutation, parent-space, and remembered-state transitions. Production
builds compile the hook away. Before old-struct specialization, fresh Dew executed
1,724 helpers (1,038 allocation, 686 mutation) versus static `hostsync=6,140`;
the per-call counts remained exact through 100 and 500 repeated calls. The
production stripped TinyGo binary was **1,780,332 bytes**, +328 bytes over the
count-only build; the tagged diagnostic product is not the authoritative release
artifact.

**Barrier-safe old/large struct reference stores (2026-08-02).** The shared AMD64
final-reference-field store stub now admits a Throughput old/large parent when the
validated child is non-young, or when a nursery child is written behind a parent
whose stable remembered bit is already set. Stores that must append remembered
metadata and every Tiny store still take the exact helper. Dew executes **426 fewer
mutation helpers per call**, reducing dynamic transitions **1,724→1,298** while
leaving the 1,038 allocation helpers unchanged. Generated code grows only **71 bytes**
(**6,957,024→6,957,095**). Across six interleaved rounds, the median of fresh
medians improves about **0.846→0.755 ms** and the median of sustained medians about
**0.880→0.839 ms**, with host allocation unchanged. The plugin-complete stripped
TinyGo candidate is **1,780,668 bytes**, +336 bytes over the helper-counter build.
The remaining 260 mutations are 258 array stores plus two unremembered old-to-young
struct stores; native array-card reconciliation remains the next write-barrier target.

**Existing-card old/large array reference stores (2026-08-02).** Collector native
ABI v2 appends a stable object-card pointer/count to the prior 112-byte prefix. The
shared AMD64 final-reference array-store stub now admits a Throughput old/large
parent only when remembered membership and a valid preallocated card slot already
exist. It widens that card interval in place and never grows collector metadata;
cardless/unremembered and Tiny stores retain exact helper fallback. Dew removes
another **254 mutation helpers per call**, reducing dynamic transitions
**1,298→1,044** and mutations **260→6**. Generated code grows **226 bytes**
(**6,957,095→6,957,321**). Across six interleaved rounds against the old-struct
build, the median of fresh medians improves about **0.671→0.660 ms** and sustained
about **0.763→0.725 ms**, with host allocation unchanged. The plugin-complete
stripped TinyGo candidate is **1,778,388 bytes**, 2,280 bytes smaller than the
preceding old-struct candidate despite the 16-byte collector-view growth. The
remaining six mutation helpers are precisely the metadata-creating cold stores.
Tagged allocation-family counters split the remaining 1,038 calls into **1,026
fully initialized `struct.new` helpers** and **12 `array.new_default` helpers**,
with zero struct-default or other-array allocations; these counts remain exact
through 500 calls. Native allocation should therefore specialize the initialized
struct path first rather than introducing a broad allocator for twelve cold arrays.

A one-ticket nursery prototype was measured and rejected. Each allocating helper
reserved one unpublished free handle plus nursery extent for one subsequent native
constructor. It reduced executed helpers **1,044→736** and initialized-struct helpers
**1,026→718**, but grew generated code **6,957,321→6,992,108 bytes** (+34,787),
reduced fresh host allocation by only 1,984 B/op with allocations unchanged, and was
roughly neutral in six noisy fresh A/B rounds (median-of-medians about
0.690→0.683 ms). More importantly, sustained repeated execution exposed a cast-failure
lifecycle bug after collection. The complete prototype was reverted.

**Transactional batched native struct allocation (2026-08-02).** The retained
replacement reserves 32 unpublished handle identities without consuming nursery
bytes. Native collector ABI v3 preserves the 128-byte v2 prefix and appends pointers
to the fixed 144-byte batch state, an explicit collection epoch, the real nursery
bump, and the semantic allocation counter. Generated code validates the complete
constructor before advancing the bump or publishing a handle; collection and Close
recycle every unused identity. Tiny, collect-every-allocation, collection-disabled,
unsupported, exhausted, and malformed cases retain the rooted helper. Dew helpers
fall **1,044→50**: initialized structs **1,026→32**, arrays remain 12, and the six
metadata-growing mutations remain unchanged. Generated code grows only **18,870
bytes** (**6,957,321→6,976,191**, +0.27%). Across six interleaved rounds, fresh
median-of-medians improves about **0.658→0.527 ms** (-20.0%) and sustained about
**0.692→0.573 ms** (-17.3%). Fresh host cost is **524,576 B/op and 69 allocations**
versus 524,512/68 disabled; sustained execution remains allocation-free. The
plugin-complete stripped TinyGo binary is **1,787,076 bytes**, +8,688 (+0.49%).
`WAGO_AMD64_NO_GC_NATIVE_ALLOC=1` restores helper-only allocation. A 64-handle
batch was also measured and rejected: it cut initialized-struct refills 32→17 per
call but made four-round fresh median-of-medians about 10% slower, added 800 B/op
and two host allocations, increased fixed collector state by 128 bytes, and was
neutral sustained. The retained 32-handle batch is the speed/footprint point.

**Bounded WasmGC load/value forwarding (2026-08-10, #314; retired).** The former
structured-fact path forwarded constructor lengths, repeated exact loads, and fresh
object stores. It was removed with the broader fact experiment after qualification
found neutral execution and materially worse compile resources. AMD64 retains only
the independent one-entry resolved-address certificate; there is no load-forwarding
or known-array-bounds policy switch.

**Exact-final specialization for open `struct.get` (2026-08-29; retired).** AMD64's
former reference-fact path lowered a scalar access declared through an open
struct supertype to the checked native final-object path when the receiver has one
proved exact final subtype, the subtype relation is valid, and both field layouts
have the same offset and scalar representation. Unknown receivers retain the
synchronous helper. On the Ryzen 7 8845HS focused guest-loop benchmark, seven
samples changed from a **78.67 ns/op median** with default facts disabled to
**2.680 ns/op** with facts enabled (**-96.6%**), at 0 B/op and 0 allocs/op. The
helper count falls from constructor plus get to constructor only; focused generated
code grows **749→906 bytes** (**+157, +21.0%**) for the complete checked resolver.
The broad facts
policy remains default-off pending compile-memory and real-workload qualification;
this result does not change that default.

**Constant-time WasmGC subtype intervals (2026-08-10, #314).** Validated collector
`TypeDesc` supertype metadata is a forest, so the collector now stores one packed
DFS `[pre,post]` interval per canonical type and uses interval containment after a
four-parent shallow fast path. `WAGO_GC_SUBTYPE_INTERVALS=0` restores complete parent
walks. The obsolete eight-byte-per-type `typeIndex` table was removed, so permanent
per-type memory is unchanged and `Collector` remains **1,120 bytes**. Three pinned-
shape 200 ms samples are neutral at depth 1 (**9.70 vs 9.75 ns/op** median), improve
depth 16 from **68.69 to 19.00 ns/op** (-72.3%), and depth 256 from **1,113 to
18.12 ns/op** (-98.4%), all 0 B/op and 0 allocs/op. Canonical-equivalence paths retain
the validated parent-chain fallback where representative remapping makes raw interval
identity inapplicable.

**Late WasmGC barrier selection and guarded bulk no-barrier fill (2026-08-10,
#315).** Structured facts now select an explicit state only after the destination
and child reach the store: `NoBarrier`, `YoungParent`, `KnownOldChild`,
`ExistingCard`, `CardMark`, or `SlowBarrier`. Compile-time null/i31 stores can omit
barrier work; object-reference stores remain on the existing native discriminator,
which preserves Throughput nursery/existing-card/card-mark paths and Tiny's complete
incremental helper. Generation remains representable in the fact format, but it does
not currently activate `YoungParent` or `KnownOldChild`: relocation validity,
concurrent marking, and remembered-set requirements must be proved independently
before those compile-time states are enabled. Reference
`array.fill` with a proven null/i31 value uses helper 35, whose runtime
`ArrayFillNoBarrier` repeats full range/type/value preflight and rejects any object
reference before mutation. Other reference fill/copy/init operations retain exact
post-write range barriers, overlap/trap atomicity, and Tiny scalar barriers;
Throughput `array.init_elem` now preflights every retained segment value, performs
barrier-deferred checked stores, and publishes one exact destination range, while
Tiny keeps immediate per-edge shading. Tiny bulk barriers process 64 elements per
chunk and drain at most 64 gray objects between chunks, bounding queued publication
work at the collector's one-object scan granularity. Numeric and function-identity
bulk operations remain barrier-free. Static barrier-state hits use
`CodegenStats.Peephole`; `wago_gcstats` snapshots now expose dynamic checked-path
counts for all six states separately. The existing native stubs remain the dynamic
source of nursery/existing-card/card-mark decisions without adding release-build
counter writes to hot code. Generated barrier/helper bytes retain separate code-size
attribution. On linux/amd64 (Ryzen 7
8845HS, Go 1.24.4, unpinned, GOMAXPROCS=16, three 200 ms samples), guarded null
reference fill improves median **37.43→33.80 ns/op** at 16 elements (-9.7%),
**53.96→51.39 ns/op** at 256 (-4.8%), and **158.2→155.9 ns/op** at 4,096
(-1.5%), all at 0 B/op and 0 allocs/op. A tertiary preflight review removed two
redundant retained-element ownership/type validations from Throughput
`array.init_elem`: the permanent deferred-batch benchmark improves median
**452.7→438.2 ns/op** at 16 elements (-3.2%), **6,740→6,560 ns/op** at 256
(-2.7%), and **108,576→104,405 ns/op** at 4,096 (-3.8%), all 0 B/op and
0 allocs/op. The explicit barrier-state matrix reports medians of **25.50 ns/op**
for nursery, **34.03** remembered old, **41.16** unremembered old including metadata
creation, **34.28** large, and **28.50** Tiny parents. Five 200 ms samples of bounded
fact intersection measure **4.642 ns/op median**, 0 B/op, and 0 allocs/op.

A seven-round `GOMAXPROCS=1`, 100-iteration real MoonBit WasmGC JSON workload A/B
measured structured facts at **188.752 µs/op median** versus **191.401 µs/op**
without them (-1.38%), with both at 208,779 B/op and 264 allocs/op. This focused
result did not clear the later broad compile-resource and generality gates.
One code-telemetry compile measured **294,341 versus 294,181 linked bytes** (+160,
+0.054%) while GC barrier bytes fell **4,642→4,168** and helper-call bytes fell
**74,202→72,500**. The retained external-fixture benchmark is
`BenchmarkGCOptimizationWorkload`; it records a semantic checksum, linked bytes,
barrier/helper bytes, runtime allocation, and execution time without vendoring the
payload. The result is modest and host-noisy, but deterministic code/work counters
show the intended reduction without an allocation regression.

**Trusted native-GC ABI boundaries and bounded resolver reuse (2026-08-10, #307).**
Collector ABI version 1 is now validated against Go structure sizes/offsets at collector
construction, recorded explicitly in codec version 2 generic-GC artifacts, rejected on
artifact mismatch, and validated with the immutable instance type-map/collector view
before basedata publication. AMD64 no longer reloads instance/collector versions,
local-map counts, or handle stride in every native GC access; mutable handle/backing,
space, object-extent, canonical-type, bounds, ownership, barrier, lifecycle, trap,
root, EH, tail, snapshot, and host-reentry semantics remain dynamic. `wagodebug`
retains a coarse Go-to-native immutable-view assertion.

A module-owned 229-byte noncollecting compact-handle resolver leaf is selected at
two or more candidate direct scalar sites. One-site modules keep the 351-byte inline
crossover instead of growing to 453 bytes. With reuse enabled, a single-function
module is first lowered inline and selects the island only when at least two actual
resolutions remain; this avoids charging the fixed island after address-certificate
reuse collapses a repeated run. With reuse disabled, eight sites shrink 1,669→821
bytes and 128 sites shrink 24,349→7,301 bytes. A one-entry derived-address certificate
keeps the compact local as the root and survives only mechanically safepoint-free
straight-line leaves; calls, helper/host transitions, allocations, control/EH/tail
edges, local writes, and unknown opcodes invalidate it. Eight repeated accesses
compile as one resolution plus seven reuses and shrink 821→452 bytes; eight distinct
objects in one function still select sharing and shrink 1,806→949 bytes. Ten CPU-0-
pinned 500 ms runtime samples measured medians of 317.0 ns/op default, 341.85 ns/op
with reuse disabled, and 340.6 ns/op fully inline, all 0 B/op and 0 allocs/op. A
same-command stripped plugin-complete TinyGo build grew 2,096,928→2,106,824 bytes
(+9,896, +0.472%); `compiledCodeCache` remains 64 bytes and collector/instance/native-
view layouts do not grow. Differential controls are
`WAGO_AMD64_NO_GC_SHARED_STUBS=1` and
`WAGO_AMD64_NO_GC_RESOLVE_REUSE=1`; permanent attribution is
`BenchmarkGCResolverCodeSize` plus `BenchmarkGCNativeResolverReuse`.

**Release unwind-table removal (2026-08-01).** TinyGo `-no-debug` Linux
releases do not use DWARF `.eh_frame` data for panic text or Wago's native
trap/signal path. In the historical monolithic CLI, removing that allocated
section shrank **2,000,192 to 1,742,072 bytes** (**−258,120, −12.9%**) and
removed the same mapped read-only bytes. A no-unwind panic probe still printed
its message and exited through the expected abort path; Core 3 `fib` and
malformed-module diagnostics were unchanged. Across 200 randomized cached
`wago --version` subprocesses per binary, startup was flat within noise (median
898.2 vs 899.4 µs). Removing compiler comments and the now-unused ELF
section-name/header table saved another 956 bytes in that historical product;
ELF loading uses program headers. The split release pipeline now applies the same
stripping to Linux Tiny runtime assets; their profile-specific sizes must be
measured independently.

**Retired Core 3 fixture-hash admission on structural products (2026-08-01).**
Collector-free function-identity products now rely on their existing strict
decoded structural proofs rather than additionally hashing historical
spec-generated binaries. Non-GC modules also skip the large type-subtyping
classifier entirely. On the original small-scalar Core 3 watchpoint, median
compile time fell **11.619→11.333 µs/op** (**−2.5%**), with 42,701→42,541 B/op
and 99→95 allocs/op. Historical monolithic full/engine releases each shrank by
about **6.5 KiB**. Official Core 3 behavior and strict malformed/type validation
remain unchanged; fixture identity is no longer a production gate.

**Execution-only CLI prototype (2026-08-01).** The superseded monolithic
`wago_engine` experiment measured **1,529,176 bytes** versus **1,742,072 bytes**
for the full CLI (**−212,896, −12.2%**) while retaining the Core 3 execution
engine. In the split CLI architecture, `make build-engine` is only a diagnostic
alias for the existing run-only `wago_runtime,wago_lean,wago_minimal` product.
The complete plugin-capable Standard runtime remains the authoritative product,
and split artifacts require fresh size measurements.

**Dense byte-backed section tracking (2026-08-01).** The byte-backed decoder
now tracks the standard-section ID set in one `uint16` instead of a hash map. A
13-section module decodes in **348.0–359.9 ns/op** versus **796.9–809.5 ns/op**,
with 864→776 B/op and 9→6 allocs/op; the larger decode+validate watchpoint remains
within noise. The stripped TinyGo release falls 2,000,232→2,000,024 bytes
(**−208 bytes**).

**Allocation-free SIMD feature detection (2026-08-01).** Linux SIMD admission
now scans exact `/proc/cpuinfo` tokens directly and returns after the first
complete flag set instead of lowercasing, splitting, and hashing the whole file.
On a 16-processor synthetic image the token pass measures **236.6–240.2 ns/op**,
0 B/op, and 0 allocs/op versus **22.05–22.44 µs/op**, 22,408 B/op, and 9
allocs/op. The scanner adds 64 bytes after the section-tracking change, leaving
the combined stripped TinyGo release at 2,000,088 bytes: still **144 bytes below**
the 2,000,232-byte starting point.

**Compact validator effect tables (2026-08-01).** Numeric and SIMD validation
lookup entries now store four one-byte fields instead of embedding full recursive
`ValType` descriptors. On linux/amd64 this shrinks `opEffects` and `simdEffects`
from 59,808 + 34,176 bytes to 2,136 + 2,136 bytes, cuts the stripped TinyGo
release `.bss` from 133,888 to 44,176 bytes (**−89,712 bytes, −67.0%**), and
reduces the release file from 1,983,792 to 1,983,216 bytes (gzip-9: 899,459 to
898,903). Twelve 1-second `BenchmarkDecodeValidate` samples improved from a
96,406 ns/op median to 90,917.5 ns/op (**−5.7%**) with allocations unchanged.
The four-byte layouts are test-locked so later reference-type growth cannot
silently re-inflate this hot table.

**Packed instruction names (2026-08-01).** `InstrKind.String` now slices one
5,834-byte name blob through generated `uint16` offsets instead of retaining 534
independent string headers and relocations. With identical build flags/version,
the stripped TinyGo release falls from 1,983,232 to 1,975,712 bytes
(**−7,520 bytes**; gzip-9 898,906 to 898,167), and `.rodata` falls from 303,056
to 295,616 bytes (**−7,440**). `go generate` derives the blob directly from the
authoritative `InstrKind` enum; compile-time count assertions and name/offset
coverage prevent stale generated metadata.

**Map-free core validation metadata (2026-08-01).** The seven numeric unary,
binary, compare, test, conversion, load, and store maps existed only to populate
`opEffects` during package initialization. Compact range/list initialization now
writes the same 151 tested effects directly and releases the source maps entirely.
Against the packed-name baseline, the stripped TinyGo release falls from
1,975,712 to 1,958,080 bytes (**−17,632**; gzip-9 898,167 to 894,216), `.data`
falls 47,116→36,716 bytes, and `.bss` falls 44,176→22,368 bytes. The stripped
Go `wago_lean` CLI falls 6,496,440→6,475,960 bytes. In 150 randomized subprocess
samples, `wago version` median startup fell 1.251→1.216 ms (**−2.8%**) and mean
fell 1.212→1.166 ms (**−3.8%**); validation allocations remain unchanged.

**Compact SIMD source metadata (2026-08-01).** Ten SIMD load/lane/scalar/limit/
unary/binary/ternary maps now use four-byte descriptor arrays and compact
instruction-kind lists while retaining the existing admission/effect cross-check.
The stripped TinyGo release falls another 1,958,080→1,941,720 bytes
(**−16,360**; gzip-9 894,216→889,055), with `.text` down 12,485 bytes, `.data`
down 3,768, and `.bss` down 6,336. The stripped Go lean CLI falls
6,475,960→6,459,576 bytes. Across 150 randomized TinyGo subprocess samples,
`wago version` median startup falls 0.507→0.470 ms (**−7.2%**) and mean falls
0.481→0.448 ms (**−6.8%**). All compact source/effect layouts and the 268-entry
inventory are test-locked.

**Dense prefix decoding (2026-08-01).** The FC/FB/FE/FD subopcode maps now use
six bounds-checked `InstrKind` arrays totaling 948 bytes. Ten one-second samples
show median lookup wins of **5.7%** for FC, **3.6%** for FB, **7.9%** for FE
memory, **21.3%** for core SIMD FD, and **25.2%** for relaxed-SIMD FD; end-to-end
`BenchmarkDecodeValidate` improves 91,178→90,607.5 ns/op (**−0.6%**). The
stripped TinyGo release falls 1,941,720→1,926,360 bytes (**−15,360**; gzip-9
889,055→882,596), and the stripped Go lean CLI falls 6,447,252→6,443,156 bytes.
TinyGo `wago version` median startup falls 0.478→0.462 ms (**−3.4%**, n=150).
Populated-entry counts and exact 948-byte storage are test-locked.

**Compact atomic validation (2026-08-01).** The seven load and seven store
validation effects now share two-byte array entries instead of full `ValType`
map values. `BenchmarkValidateAtomicEffects` improves 12,320.5→10,400 ns/op
(**−15.6%**, 224 lookups/op, 12 one-second samples). The stripped TinyGo release
falls 1,926,360→1,923,896 bytes (**−2,464**; gzip-9 882,596→882,035), `.bss`
falls 10,960→9,040 bytes, and the stripped Go lean CLI falls
6,443,156→6,439,060 bytes. TinyGo startup shifts 0.453→0.449 ms median
(**−0.8%**, n=150). Layout, effect identity, range rejection, and counts are
test-locked.

**Allocation-free SIMD admission scans (2026-08-01).** The frontend now queries
the validator's compact SIMD effect array directly instead of constructing a
268-entry map at every recursive instruction-list scan. On a 256-instruction
non-SIMD body, `BenchmarkInstrsRequireSIMD` improves 4,600,977→1,653.5 ns/op,
2,097,833→0 B/op, and 2,307→0 allocs/op; finding SIMD in the last slot improves
4,471,060.5→1,641 ns/op with the same zero-allocation result. The stripped
TinyGo release falls 1,923,896→1,921,848 bytes (**−2,048**; gzip-9
882,035→880,352), `.bss` falls 9,040→6,896 bytes, and the stripped Go lean CLI
falls 6,439,060→6,434,964 bytes. TinyGo startup falls 0.423→0.396 ms median
(**−6.4%**, n=150). Admission/snapshot parity, out-of-range rejection, and the
zero-allocation scan are test-locked.

**Dense section ordering (2026-08-01).** The 13 standard section IDs now use a
14-byte direct-order table instead of a hash map. Thirteen lookups improve
192.05→6.0265 ns/op (**31.9×**), while a synthetic module containing every
standard section decodes in 1,028.5→822.1 ns/op (**−20.1%**, 12 one-second
samples). The stripped TinyGo release falls 1,921,848→1,920,272 bytes
(**−1,576**; gzip-9 880,352→879,889), `.bss` falls 6,896→6,544 bytes, and the
stripped Go lean CLI falls 6,434,964→6,430,868 bytes. Exact order identity,
reserved/custom rejection, and 14-byte storage are test-locked.

**Dense reverse encoding (2026-08-01).** The expression encoder now reverses
simple and memory opcodes through two fixed byte arrays instead of hash maps.
Encoding 256 simple instructions improves 4,890→1,186 ns/op (**4.1×**), and 256
memory instructions improve 6,263.5→2,139 ns/op (**2.9×**, 12 one-second
samples), with unchanged per-call allocations. This speed trade adds 32 bytes
to the stripped TinyGo ELF (1,920,272→1,920,304) while reducing gzip-9
879,889→879,411 bytes, `.text` by 860 bytes, and `.bss` by 16 bytes; the
stripped Go lean CLI is unchanged at 6,430,868 bytes. TinyGo startup is flat
within noise at 0.319→0.317 ms median (n=150). Reverse-map identity,
`unreachable` opcode zero, typed-select exclusion, out-of-range rejection, and
1,068-byte table storage are test-locked.

**Skip empty nested SIMD scans (2026-08-01).** The frontend now checks nested
body/then/else lengths before recursive SIMD admission calls. On a flat
256-instruction body, absent-SIMD scanning improves 1,635.5→573.25 ns/op
(**−65.0%**) and last-position SIMD improves 1,638→576.45 ns/op (**−64.8%**),
remaining at zero allocations. The stripped TinyGo ELF adds 16 bytes
(1,920,304→1,920,320), gzip-9 shifts 879,411→879,409 bytes, and the stripped Go
lean CLI remains 6,430,868 bytes. Existing byte-backed and structured nested
expression coverage preserves recursive semantics.

**Dense backend ALU metadata (2026-08-01).** amd64 and ARM64 now select the five
basic integer ALU encodings through fixed arrays instead of hash maps. On amd64,
256 lookups improve 2,783.5→344.05 ns/op (**8.1×**); a synthetic 512-add
function compiles in 60,869→59,467 ns/op (**−2.3%** median, 12 one-second
samples), while the medium-control compile benchmark remains flat. The stripped
TinyGo release falls 1,920,320→1,919,680 bytes (**−640**; gzip-9
879,409→878,931); the stripped Go lean CLI remains 6,430,868 bytes. Exact amd64
and ARM64 encoding identity and 24/12-byte storage are test-locked.

**Fixed runtime metadata without maps (2026-08-01).** Trap-code strings now use
a 20-entry array, and the 12 reserved `wago_*` import namespaces use a static
string switch. Across 256 lookups, trap formatting improves 1,289→250.9 ns/op
(**5.1×**) and reserved-module classification improves 1,944.5→394.6 ns/op
(**4.9×**, 12 one-second samples), both allocation-free. The stripped TinyGo
release falls 1,919,680→1,918,600 bytes (**−1,080**; gzip-9 878,931→878,298),
`.bss` falls 6,544→5,824 bytes, and the stripped Go lean CLI remains 6,430,868
bytes. Exact trap messages, 320-byte storage, and reserved/non-reserved names
are test-locked.

**Earlier metadata campaign total:** from `19d000ba` through fixed runtime
metadata, the stripped TinyGo release shrank 1,983,792→1,918,600 bytes
(**−65,192, −3.3%**), gzip-9
shrinks 899,459→878,298 bytes, and `.bss` shrinks 133,888→5,824 bytes
(**−128,064, −95.6%**). The latest stable randomized subprocess median for
TinyGo `wago version` is about 0.4 ms versus the pre-campaign 0.549 ms;
`BenchmarkDecodeValidate` remains **6.3%** below the pre-campaign median.

## Earlier implementation record

The AMD64 backend (`src/core/compiler/backend/railshot/amd64`) is the full
WARP-architecture port: single-pass x86-64 codegen over a valent-block operand
stack (deferred-action trees, condense engine) with an on-the-fly
whole-register-file allocator. ARM64 has an architecture-specific direct backend;
has parity status summarized below. Landed, in rough order:

### Storage model and register allocation
- **Register-ABI internal calls** (old P1) — args/results in registers between wasm
  functions; wrapper ABI kept at the Go boundary. Includes the parallel-move resolver.
- **Hotness-aware local pinning** (old P2) — loop-weighted scores from a one-pass
  `scanBody` pre-scan (`hints.go`), WARP-style whole-file pin pool for call-making
  functions too (up to `file − 4 scratch`), STACK_REG lazy spill (dirty-only stores at
  calls, lazy reload) for **all** call-making functions. #68's real root cause (the
  `opElse` merge edge skipping reconciliation) was found and fixed with regression tests.
- **Value-pinned hot globals** sharing the pin pool (#84–#86).
- **memBytes in R15** (old P3) — explicit-bounds mode keeps the memory size in a
  module-wide reserved register (WARP `REGS::memSize`); checks are `lea; cmp; ja stub`.
- **Lazy per-frame merge agreement** (old P6, locals half) — control-flow edges agree
  per-frame on each pinned local's merge state (`lsStackReg` or `lsMem`), so a
  call-clobbered local can stay slot-only across a merge until actually read. Loop tops
  stay eager (reloads hoisted out of bodies). Conditional returns converge nothing.

### Bounds checks and traps
- **Guard-page mode** (old P5) is first-class behind `-tags wago_guardpage` and is the
  *default* bounds mode in such builds (`WAGO_BOUNDS=explicit` overrides).
- **Shared cold trap stubs** (old P9) — one stub per trap code per function; every check
  is a fall-through `ja stub`. (~23% smaller code on memory-heavy modules.)
- **Stack-fence elision for small call-free leaves** — a leaf's one unchecked frame is
  absorbed by the fence's 256 KiB margin.

### Instruction selection
- Compare→branch fusion; constant folding; memarg offset folding; deferred loads folded
  as ALU r/m operands; in-place accumulation; cmov select.
- **Algebraic identities + strength reduction** (old P4) — `x±0`, `x&~0`, `x|0`, `x^0`,
  shifts by 0, `x*1`, `x*0`, `x*2ⁿ→shl`, `x*{3,5,9}→lea`, `x/ᵤ2ⁿ→shr`, `x%ᵤ2ⁿ→and`,
  `x-x`/`x^x→0`, `x&x`/`x|x→x` — at `pushBinOp`, before a node exists.
- **Const-fold pack** (P2.3/P2.4) — extends constant folding past binary arithmetic to
  relational compares (all i32/i64 signed+unsigned), the unary ops (clz/ctz/popcnt/eqz),
  and the width conversions (wrap / sign- & zero-extend); plus same-local integer compare
  identities (`x==x`/`x<=x`/`x>=x→1`, `x!=x`/`x<x`/`x>x→0`) alongside the existing
  `x-x`/`x&x` ones. All at `pushBinOp`/`pushUnOp`, fire only on compile-time-known inputs
  (`const-fold` / `same-operand` counters), so no node/SETcc is emitted (`fold.go`).
- **Packed-word mask tests** — Lamport-style `(word & laneMask) == 0` predicates
  lower directly to `TEST` (amd64) or `TST` (arm64), avoiding the temporary masked
  value (`swar-mask-test`). The direct fusion has no solver, cache, persistent IR,
  or tree walk; `WAGO_NO_SWAR_MASK_TEST=1` is its A/B oracle. Earlier recursive
  known-bits analysis and producer-shaped SWAR widen/pack/parse and multiply-high
  recognizers were removed after corpus censuses found no independent-producer hits.
- **Bounded SIMD superops** — the same offline-discovery/online-selection split now
  covers exact adjacent Wasm SIMD operations without retaining a SIMD IR. The first
  selectors fold `v128.not; v128.and` to one `VPANDN`/`BIC`, and fold
  `v128.and; v128.any_true` to `VPTEST; SETNE` on AMD64 or a directly owned NEON
  AND/reduction on ARM64. Lookahead is two bytecode operations, allocation-free, and
  restored on every near miss. `WAGO_NO_SIMD_SUPEROPT=1` is the differential A/B oracle.
- **Scaled-index LEA fusion** — `add(x, shl(y, k≤3))` → `lea [x + y*2ᵏ]` (the
  AssemblyScript array-address shape).
- **`br_table` jump tables** (old P7) — n≥5 dispatches through a RIP-relative offset
  table with deduplicated per-case stubs; smaller tables keep the cmp/jne chain.
- **Small constant `memory.fill`/`copy` unrolled** — n≤32 lowers to overlap-safe
  load-all/store-all chunks (memmove semantics preserved); no `rep` microcode startup.
- **`call; local.set` result fusion** — a register-ABI call result lands directly in the
  pinned local's register.
- **Register-ABI `call_indirect`** — the table entry's pad word carries the internal-entry
  delta, so compatible signatures skip the wrapper adapter.
- **Code layout** — 16-byte aligned functions, internal entries, and loop tops (multi-byte
  NOPs on the entry path). Tight-loop benchmarks swing ±20% on layout luck without this;
  treat any single-module regression as suspect until the disassembly is diffed.

### MVP completeness

The old "completion batch" includes memory.grow/size, trapping float→int
truncation + trunc_sat, start function, multi-value, imported/mutable globals.

### AMD64 corpus pass (2026-07-30)
- Float-local residency now uses WARP's eleven-register pool in call-making
  functions as well as leaves; the existing dirty spill/lazy-reload protocol
  preserves the caller-saved XMM pins.
- Scalar `f32`/`f64` add and multiply fold deferred linear-memory operands into
  non-destructive VEX instructions, avoiding a preserve-source move.
- Explicit bounds mode keeps eight fixed-size, round-robin straight-line
  certificates so interleaved array accesses do not evict one another.
- AMD64 cancellation safepoints poll a direct basedata mirror. The runtime keeps
  that cell synchronized with the stable trap buffer, including cross-instance
  calls, without a cache or per-call allocation.
- SIMD lowering now uses compact VEX encodings, immediate packed shifts, exact
  rotate recognition, native/common-source shuffles, packed memory operands,
  direct pinned-local sinks, live `local.tee` forwarding, and exact dead tee-store
  elimination. These are bounded instruction-selection rules, not a value cache.
- The byte-body scan identifies declared locals assigned before their first read
  in the straight-line entry prefix, allowing their dead prologue zeroing to be
  omitted. CRC-style table indices additionally use nested scaled-index LEA and
  `(x ^ load8_u) & 255` lowers to `movzx` plus a low-byte XOR.

On a Ryzen 7 7800X3D, Linux/amd64, Go 1.22.2, explicit-bounds `BenchmarkKernels` pinned
to CPU 7 with `GOMAXPROCS=1` (`-count=6 -benchtime=1s`) measured median execution
changes of nbody 295.7→281.0 µs (-5.0%), matmul 168.8→138.3 µs (-18.1%), and
raytrace 412.1→383.5 µs (-6.9%). All remained 0 B/op and 0 allocs/op. The fixed
footprint cost is 16 additional basedata bytes per memory and 8 additional trap
buffer bytes per instance.

The SIMD/integer follow-up was measured sequentially from separate worktrees at
`origin/main` (`67ac1c25`) and this branch, one corpus at a time. Seven 2-second
`BenchmarkExec` samples gave BLAKE SIMD 901.2→631.9 µs (-29.9% latency,
+42.6% throughput) and CRC32 20.416→16.447 µs (-19.4% latency, +24.1%
throughput). Five 1-second samples gave JSON SIMD serialize 29.963→28.750 µs
(-4.0%) and UTF SIMD 64.754→61.518 µs (-5.0%). Every result remained 0 B/op and
0 allocs/op. The BLAKE SIMD result is also checked under both bounds modes and
against wazero by the corpus runner; the lowering choices retain WARP's bounded
register-residency model while using AVX's non-destructive three-operand forms.

### Compile speed

Decoded modules keep byte-backed function bodies. The optional
`scanBody` instruction walk is used only for programmatically constructed modules that
provide decoded instructions; normal decoded modules use BodyBytes and first-N pinning.
Validation is byte-backed and no-body too (#96: type-cache + validator/reader reuse,
validation allocs −90%). The opt-in function-worker policy now applies to both
validation and codegen: module-wide work remains serial, function
bodies fan out with worker-local state, and results/errors rejoin deterministically.
At p4, validation improved 58–72% on the representative medium/large corpus while
keeping serial allocation counts unchanged.

### Landed since the #87/#88 sweep (2026-07-02 to 2026-07-03)
- **Borrowed reads for value-pinned globals** (`stGlobReg`, #93) and
  **immediate-only constant stores** (`StoreImmIdx`, #94).
- **Float parity batch** (#97): `minss/maxss`-based min/max with NaN fixup + deferred
  float loads (VEX 3-op float ops were #79).
- **Forward `rep movsb` for disjoint `memory.copy`** (#99) — the json serialize fix; the
  backward-copy path gets no ERMSB/FSRM and was ~89% of serializer samples.
- **Adaptive per-module global-pin K (1–3)** (#100) and **`x*{3,5,9}` → LEA** (#101).
- **Instantiate reuse** (#105 explicit, #108 guard: 12→3.4µs) and faster validation (#96).
- **Full wasm 1.0 MVP: 57/57 spec files, 0 failing assertions** (#111–#115: spectest host
  module, cross-instance function/global/table/memory linking, host functions as table
  funcrefs).

### Measured (2026-07-02, explicit bounds, vs the pre-sweep #87 baseline)

| bench | #87 | sweep | Δ |
|---|---:|---:|---:|
| sieve | 163µs | 123µs | **−24%** (beats wazero) |
| memory_tree | 14.6µs | 11.8µs | **−19%** |
| linked_list | 11.3µs | 9.4µs | **−17%** |
| dispatch (call_indirect) | 19.1ns | 17.6ns | −8% |
| blake-as | 729µs | 700µs | −4% |
| json-as ser / deser | 218 / 396 | 197 / 204 | −10% / **−48%** |
| memory.sum (explicit vs guard) | 337 | 230 | **explicit == guard** |

Cumulative from before #87 (main@22c09be): json ser 257→197, deser 420→204;
memory.sum 552→230; sieve 165→95; memory_tree 17.2→11.6; wazero-relative json
0.56x→0.72x ser / **0.70x→1.43x deser (wago now wins)**. wago beats wazero on
fib_rec, sieve, memory_tree, linked_list, dispatch, branches, and json deserialize;
loses on json serialize and blake.

The deserialize flip came from running WARP itself on json-as (passive/bounds-off
build, ser 97ns / deser 164ns per unit) and replicating its remaining structural
edges: no per-call environment protocol (RBX/linMem as module invariant, trap cell
in basedata — no trap-clear on returns), module-wide global register pinning (the
AS shadow-stack pointer), pinned-register-borrowed load addresses, and — decisive
for deserialize — an inline 8-byte chunk-loop memmove for small dynamic
memory.copy/fill instead of `rep movsb` (whose startup latency dominated the
string-append copies AssemblyScript's `__renew` makes constantly). wago-guard
deser is now within 1.13× of WARP.

**Update (post #97/#99/#100/#101, guard mode):** json ser **93ns / deser 175ns**
per unit — **serialize now beats WARP (97)**; deser is 1.07× WARP (164). wago
beats wazero (147/305) on both json directions. The serialize chase is closed;
see R4.

### Curated-idiom A/B (2026-07-17, Apple M4 Max, darwin/arm64)

Five repeated 500 ms samples, explicit bounds. Medians are shown; compilation memory is
unchanged because the matchers use the existing reader and deferred nodes.

| workload | idioms off | idioms on | change | memory |
|---|---:|---:|---:|---:|
| utf-as backend compile | 100.0 us | 102.1 us | +2.1% | 178,256 B / 161 allocs (same) |
| utf-as full compile | 240.8 us | 241.8 us | +0.4% | 228,522 B / 240 allocs (same) |
| utf-as `convertN(200)` | 116.6 us | 107.0 us | **−8.2%** | 0 B / 0 allocs per call |
| xjb fixture backend compile | 10.07 us | 9.46 us | **−6.1%** | 28,968 B / 53 allocs (same) |
| xjb fixture full compile | 22.14 us | 21.28 us | **−3.9%** | 36,035 B / 111 allocs (same) |
| native `mulhi64` execution | 5.17 ns | 4.54 ns | **−12.2%** | 0 B / 0 allocs per call |

Generated function code shrinks by 16 B for utf-as's matched decoder function
(3448→3432 B) and by 72 B for the xjb multiply-high export (168→96 B). The isolated
ARM64 widen microbenchmark is smaller but slower (4.25→4.57 ns); the real utf-as result
is the acceptance signal because the widened value feeds its surrounding decoder loop.

### Native AMD64 A/B (2026-07-18, Ryzen 7 7800X3D, linux/amd64)

Seven repeated 1 s samples on the remote native AMD64 host. These are raw medians from
sequential off/on runs; no-hit json-as controls moved +1–3%, so compile changes in that
range should be treated as noise rather than attributed to the selectors.

| workload | idioms off | idioms on | change | memory |
|---|---:|---:|---:|---:|
| utf-as backend compile | 111.46 us | 114.16 us | +2.4% | 179,200 B / 156 allocs (same) |
| utf-as full compile | 300.34 us | 304.30 us | +1.3% | 229,474 B / 235 allocs (same) |
| utf-as `convertN(200)` | 185.92 us | 181.43 us | **−2.4%** | 0 B / 0 allocs per call |
| xjb fixture backend compile | 11.114 us | 11.051 us | −0.6% | 28,144 B / 49 allocs (same) |
| xjb fixture full compile | 26.138 us | 25.569 us | **−2.2%** | 35,201 B / 107 allocs (same) |
| xjb exported `mulhi64` | 14.58 ns | 14.01 ns | **−3.9%** | 0 B / 0 allocs per call |

AMD64 generated code shrinks by 32 B in utf-as's matched decoder function
(4694→4662 B). The xjb multiply-high export shrinks by 112 B (201→89 B), eliminates
its 72 B frame and spill, and passes native execution, liveness, and near-miss tests.

### utf-as pack / broadword fixture A/B (2026-07-18)

Five repeated 1 s samples per mode. The focused fixture preserves utf-as's exact
four-halfword low-byte pack plus json-as's unchecked four-digit fold. At this point only
the pack was selected: rewriting the digit fold lane-wise would change its cross-lane
carry behavior for arbitrary inputs. The follow-up below instead selects the complete
expression and preserves that behavior exactly.

| host / workload | idioms off | idioms on | change | memory |
|---|---:|---:|---:|---:|
| M4 Max backend compile | 10.523 us | 10.049 us | **−4.5%** | 29,568 B / 72 allocs (same) |
| M4 Max full compile | 22.000 us | 21.937 us | −0.3% | 36,851 B / 135 allocs (same) |
| M4 Max exported `pack` | 21.77 ns | 20.88 ns | **−4.1%** | 0 B / 0 allocs |
| M4 Max mixed `runN(1000)` | 1.300 us | 1.019 us | **−21.6%** | 0 B / 0 allocs |
| Ryzen backend compile | 12.576 us | 11.923 us | **−5.2%** | 28,440 B / 60 allocs (same) |
| Ryzen full compile | 28.035 us | 27.444 us | **−2.1%** | 35,721 B / 127 allocs (same) |
| Ryzen exported `pack` | 13.56 ns | 13.69 ns | +1.0% (host-call noise) | 0 B / 0 allocs |
| Ryzen mixed `runN(1000)` | 1.926 us | 1.630 us | **−15.4%** | 0 B / 0 allocs |

On ARM64, the pack export shrinks 116→72 B and the mixed loop 260→220 B; on AMD64
they shrink 119→78 B and 256→219 B. The matcher
also runs when AssemblyScript removes the final `i32.wrap_i64`, preserving the exact
zero-extended i64 result. Native AMD64 measurements are recorded from the Ryzen host
alongside the other AMD64 broadword numbers above.

### Native SIMD superop A/B (2026-07-18)

The checked-in utf-as SIMD entrypoint now exercises both length and validation paths.
Five compile samples and seven validation-execution samples were taken on the Ryzen
7 7800X3D host; medians are shown. The M4 Max uses the same matcher and passes the
same differential tests, but its AND + ANY_TRUE NEON instruction sequence is already
minimal, so its validation runtime remains flat (361.1 vs 361.7 us).

| workload | superops off | superops on | change | memory |
|---|---:|---:|---:|---:|
| AMD64 utf-as SIMD backend compile | 397.24 us | 396.96 us | −0.1% (flat) | 262,267 B / 523 allocs (same) |
| AMD64 utf-as SIMD full compile | 957.88 us | 956.91 us | −0.1% (flat) | 433,980 B / 953 allocs (same) |
| AMD64 utf-as SIMD `validateN(200)` | 187.34 us | 183.61 us | **−2.0%** | 0 B / 0 allocs |

Five real utf-as SIMD sites select. AMD64 generated code shrinks by 138 B across the
three hit functions (8056→8014 B, 4444→4380 B, 2628→2596 B). Focused exact-pattern
tests shrink AND + ANY_TRUE by 21 B (124→103 B) and NOT + AND by 10 B (152→142 B),
with arbitrary-bit inputs, non-adjacent near misses, and kill-switch equivalence covered.

### Research-guided reduction follow-up (2026-08-04)

The follow-up keeps Railshot's bounded, exact selector model. Souper and Minotaur's
useful lesson here is to match a small functional DAG and prove the replacement over
the full input domain, rather than attach optimistic range assumptions. The broadword
papers supply the multiplication-based lane-collapse shape; Valent Blocks supplies the
existing deferred expression tree on which the matcher operates.

ARM64 now recognizes json-as's complete unchecked four-lane decimal fold. Two `MADD`
instructions expose the multiply-plus-add reductions while retaining the original
cross-lane carries for every i64 input. Adversarial arbitrary-bit tests, including a
case that disproved an earlier independent-lane candidate, protect that requirement;
a one-constant near miss does not select. The focused function shrinks **140→124 B**,
and the mixed `runN(1000)` fixture shrinks **252→240 B**. Seven native 2 s samples on
an Apple M4 Max improve the latter from **1.030→1.004 us median** (**−2.5%**), with
0 B/op and 0 allocs/op.

AMD64's general `v128.any_true` lowering now uses `VPTEST x,x; SETNE` directly instead
of constructing a zero vector, comparing bytes, extracting a movemask, and comparing
that mask. Six standalone sites in utf-as SIMD select in addition to the five existing
AND + ANY_TRUE superops, removing **96 native-code bytes** across the two affected
functions (6952→6936 B and 2450→2370 B). A 64-reduction throughput watchpoint, measured
as seven 2 s samples under linux/amd64 Rosetta (`VirtualApple`, GOMAXPROCS=1), improves
from **57.21→39.59 ns/op median** (**−30.8%**), with 0 B/op and 0 allocs/op. This is a
focused instruction-throughput result, not a whole-library claim: five matched 1 s
`utf-as-simd.validateN(200)` samples are flat at **320.484→319.610 us** (**−0.3%**).
The complete official SIMD proposal suite remains green at 470 modules and 24,325
assertions with zero failures, skips, or gaps on linux/amd64.

### Known-bits and producer-shaped SWAR removal (2026-09-03)

The recursive known-bits mask simplifier was removed after it fired only four
times in the representative corpus, all in utf-as. A later full-corpus census
found the handwritten SWAR widen/pack/parse and multiply-high recognizers only in
json-as, utf-as, their focused synthetic fixture, and the xjb-mulhi fixture. With
no independent-producer hits, the recognizers, internal operations, emitters,
public optimization flag, and focused tests were removed. Direct packed-mask
`TEST`/`TST` fusion remains because it is a small general instruction-selection
rule rather than a producer sequence.

Backend compile measurements used exact `main`, PR-head, and optimized binaries
with `GOMAXPROCS=1`. The Apple M4 Max rows are five interleaved 300 ms samples;
the Ryzen 7 7800X3D rows are six interleaved 1 s samples pinned to CPU 7. Medians
put the low-memory tree 0.5-2.6% behind `main`, while remaining faster than PR
head in every measured row:

| host | workload | main | PR head | low-memory final |
|---|---|---:|---:|---:|
| M4 Max / ARM64 | json-as | 702.73 us | 714.85 us | 706.36 us |
| M4 Max / ARM64 | blake-as | 171.44 us | 176.94 us | 175.91 us |
| M4 Max / ARM64 | utf-as | 103.59 us | 107.81 us | 104.64 us |
| Ryzen / AMD64 | json-as | 914.42 us | 928.28 us | 926.25 us |
| Ryzen / AMD64 | blake-as | 183.08 us | 189.54 us | 186.28 us |
| Ryzen / AMD64 | utf-as | 127.63 us | 131.65 us | 129.25 us |

Compile memory is now exactly back to `main` on both hosts. Against PR head,
json-as drops 3,696 B and 33 allocations per compile, while utf-as drops 1,680 B
and 15 allocations. On ARM64, the focused `swar-pack-parse` fixture also returns
47,392 B / 77 allocs to main's 46,720 B / 71 allocs, and utf-as SIMD returns
334,840 B / 357 allocs to 333,720 B / 347 allocs. Blake-as and xjb-mulhi were
already equal to main and remain so. Against `main`, ARM64
`convertN(200)` moves from 123.14 us to 105.24 us and total utf-as native code
shrinks 4432 to 4352 bytes. AMD64 `convertN(200)` moves from 195.58 us to
167.49 us, remains 0 B/op and 0 allocs/op, and total utf-as native code shrinks
5052 to 4956 bytes. Relative to PR head alone, the estimator removal shrinks the
ARM64 function by another 16 bytes and grows the AMD64 function by 8 bytes. The
small AMD64 code-size cost is retained in exchange for the simpler compile path,
main-level compile allocation, and the second ARM64 selector hit.

### SWAR versus SIMD corpus comparison (2026-07-18)

Five repeated 500 ms samples per row, explicit bounds, from the same
`b9875a2` tree. Values are medians. `Compile` starts from an already decoded and
validated module; `CompileFull` is the public decode + validate + compile path.
Instantiation starts from an already compiled module. Native code size is the sum of
the function bodies reported by `bench/cmd/explain`.

The pairs are the checked-in AssemblyScript SWAR and SIMD corpus programs with matching
manifest exports and iteration counts. They are representative end-to-end library
choices, not a one-instruction A/B: the Wasm programs differ in size and structure.
The focused superop A/B above isolates Railshot's selector impact.

#### Artifact and native code size

| library | mode | Wasm | ARM64 native code | AMD64 native code |
|---|---|---:|---:|---:|
| json | SWAR | 22.9 KiB | 61.8 KiB | 70.8 KiB |
| json | SIMD | 25.0 KiB | 71.5 KiB | 83.1 KiB |
| blake | SWAR | 4.6 KiB | 10.8 KiB | 14.9 KiB |
| blake | SIMD | 22.9 KiB | 65.0 KiB | 67.3 KiB |
| utf | SWAR | 19.7 KiB | 3.8 KiB | 5.1 KiB |
| utf | SIMD | 29.1 KiB | 30.5 KiB | 23.7 KiB |

#### Pipeline latency

| host | library | mode | decode | validate | backend compile | full compile | instantiate |
|---|---|---|---:|---:|---:|---:|---:|
| M4 Max / ARM64 | json | SWAR | 62.36 us | 284.26 us | 691.87 us | 1569.95 us | 1.51 us |
| M4 Max / ARM64 | json | SIMD | 69.34 us | 325.71 us | 775.38 us | 1738.82 us | 1.54 us |
| M4 Max / ARM64 | blake | SWAR | 16.94 us | 88.44 us | 177.44 us | 421.35 us | 0.81 us |
| M4 Max / ARM64 | blake | SIMD | 73.60 us | 371.65 us | 661.52 us | 1651.17 us | 0.82 us |
| M4 Max / ARM64 | utf | SWAR | 14.45 us | 50.85 us | 101.40 us | 246.23 us | 1.71 us |
| M4 Max / ARM64 | utf | SIMD | 35.92 us | 175.10 us | 322.03 us | 733.42 us | 1.99 us |
| Ryzen / AMD64 | json | SWAR | 67.63 us | 317.46 us | 886.15 us | 1992.46 us | 2.00 us |
| Ryzen / AMD64 | json | SIMD | 76.15 us | 365.59 us | 1038.03 us | 2150.81 us | 2.05 us |
| Ryzen / AMD64 | blake | SWAR | 17.57 us | 102.76 us | 187.00 us | 499.32 us | 1.13 us |
| Ryzen / AMD64 | blake | SIMD | 78.88 us | 426.76 us | 848.64 us | 2069.97 us | 1.14 us |
| Ryzen / AMD64 | utf | SWAR | 18.50 us | 63.16 us | 118.01 us | 303.47 us | 2.25 us |
| Ryzen / AMD64 | utf | SIMD | 42.01 us | 196.13 us | 395.49 us | 961.04 us | 2.61 us |

#### Compile and instantiate memory

`B/op` measures total allocation volume for the operation, not retained heap. Execution
memory is reported separately below.

| host | library | mode | backend compile | full compile | instantiate |
|---|---|---|---:|---:|---:|
| M4 Max / ARM64 | json | SWAR | 322.1 KiB / 1287 allocs | 482.7 KiB / 1640 allocs | 2.8 KiB / 14 allocs |
| M4 Max / ARM64 | json | SIMD | 342.0 KiB / 1478 allocs | 545.8 KiB / 1952 allocs | 2.8 KiB / 14 allocs |
| M4 Max / ARM64 | blake | SWAR | 197.4 KiB / 166 allocs | 214.7 KiB / 283 allocs | 1.9 KiB / 9 allocs |
| M4 Max / ARM64 | blake | SIMD | 487.0 KiB / 280 allocs | 640.6 KiB / 768 allocs | 1.9 KiB / 9 allocs |
| M4 Max / ARM64 | utf | SWAR | 175.2 KiB / 176 allocs | 224.3 KiB / 255 allocs | 1.3 KiB / 9 allocs |
| M4 Max / ARM64 | utf | SIMD | 244.4 KiB / 344 allocs | 413.9 KiB / 775 allocs | 1.7 KiB / 14 allocs |
| Ryzen / AMD64 | json | SWAR | 330.1 KiB / 1434 allocs | 399.5 KiB / 1781 allocs | 2.8 KiB / 14 allocs |
| Ryzen / AMD64 | json | SIMD | 349.9 KiB / 1691 allocs | 451.5 KiB / 2158 allocs | 2.8 KiB / 14 allocs |
| Ryzen / AMD64 | blake | SWAR | 193.6 KiB / 179 allocs | 210.9 KiB / 301 allocs | 1.9 KiB / 9 allocs |
| Ryzen / AMD64 | blake | SIMD | 514.9 KiB / 467 allocs | 668.6 KiB / 959 allocs | 1.9 KiB / 9 allocs |
| Ryzen / AMD64 | utf | SWAR | 175.8 KiB / 172 allocs | 224.9 KiB / 256 allocs | 1.2 KiB / 9 allocs |
| Ryzen / AMD64 | utf | SIMD | 256.1 KiB / 523 allocs | 423.8 KiB / 953 allocs | 1.7 KiB / 14 allocs |

#### Execution latency and memory

| host | workload | SWAR | SIMD | SIMD change | memory |
|---|---|---:|---:|---:|---:|
| M4 Max / ARM64 | `json serializeN(200)` | 19.18 us | 24.65 us | +28.5% | 0 B / 0 allocs |
| M4 Max / ARM64 | `json deserializeN(200)` | 38.48 us | 49.78 us | +29.4% | 0 B / 0 allocs |
| M4 Max / ARM64 | `blake hashN(100)` | 451.49 us | 634.88 us | +40.6% | 0 B / 0 allocs |
| M4 Max / ARM64 | `utf convertN(200)` | 107.36 us | 143.87 us | +34.0% | 0 B / 0 allocs |
| M4 Max / ARM64 | `utf validateN(200)` (SIMD-only) | - | 361.55 us | n/a | 0 B / 0 allocs |
| Ryzen / AMD64 | `json serializeN(200)` | 25.17 us | 29.96 us | +19.0% | 0 B / 0 allocs |
| Ryzen / AMD64 | `json deserializeN(200)` | 45.25 us | 57.37 us | +26.8% | 0 B / 0 allocs |
| Ryzen / AMD64 | `blake hashN(100)` | 884.18 us | 892.33 us | +0.9% | 0 B / 0 allocs |
| Ryzen / AMD64 | `utf convertN(200)` | 173.22 us | 63.82 us | **-63.2%** | 0 B / 0 allocs |
| Ryzen / AMD64 | `utf validateN(200)` (SIMD-only) | - | 188.85 us | n/a | 0 B / 0 allocs |

For these artifacts, SWAR is the better default on ARM64 and for JSON on both hosts:
it is faster in seven of the eight matched host/workload rows, while also producing
smaller modules and cheaper validation/compilation. SIMD is decisively worthwhile for
UTF conversion on AMD64, where it is 63.2% faster. BLAKE SIMD is effectively tied on
AMD64 execution but pays roughly 4x full-compile latency and 3.2x allocation volume;
its current entrypoint does not earn that footprint cost. All measured execution paths
remain allocation-free.

---

## Remaining roadmap (priority-ordered)

This document is authoritative for current optimization status. R-numbers remain
stable labels and Pn identify the original plan phases.

### R0. `CodegenStats` + explain mode  · ✅ LANDED (`perf/codegen-stats`)
Per-function counters (spills/flushes/condenses/store-forced deferred loads/bounds
checks/trap stubs/calls by kind/pins/peephole hits) collected only on request via a
nil-safe `stats` field on `fn` — byte-identical codegen when off. Surfaced through
`CompileOptions.Stats`, `WAGO_EXPLAIN=1`, and `bench/cmd/explain`; ships the
`WAGO_DEBUG_MODGLOBALS` / `WAGO_PIN_GLOBAL_K=auto|0..3` knobs and an objdump-based
golden-disasm harness (`golden_test.go`). Every subsequent optimization lands with
its counter moving and a golden. Plan P1 defines the counter list and
on-corpus verification.

### R1. `stFlags` — compare fusion past adjacency  · M · 🟩 (old P8)
**`eqz`-of-compare fusion LANDED** (`perf/railshot-stflags`, gated `WAGO_NO_STFLAGS`):
`condenseToFlags` peels `eqz` wrappers around a fusable compare and INVERTS the branch
condition instead of materializing the inner boolean — `eqz(a<b); if` → `cmp; jcc`
(inverted) rather than `cmp; setcc; movzx; test; jz`. Nested `eqz` double-inverts. The
inner CMP is still emitted last, so flag safety is unchanged (no register/merge hazard).
This was the dominant realizable slice: **esbuild 26,344 folds, compare-setcc 44,495→18,151
(−59%), −272 KB code (0.84%)**; sqlite 425. Verified: spec suite (16,022 asserts) + full
corpus/WASI differential + A/B kill switch.

**Premise correction (profiled 2026-07-08):** the roadmap framed the flags-resident
*storage* + local round-trip (`cmp; local.tee $c; br_if`, `eqz; local.set/get; if`) as the
unlock. Corpus scan refuted it: adjacent `compare→branch` already fuses ~99% (compilers
emit the compare adjacent to its branch), and the round-trip patterns barely exist
(`cmp;set;get;br` = **0** across esbuild/sqlite/lua/json; `cmp;tee;br` = 0/72/22/0). The big
`compare-setcc` counts are overwhelmingly GENUINE booleans (stored/returned/used in
arithmetic), not missed fusions — EXCEPT `eqz`-of-compare, which is huge and is what landed.
The full flags-resident storage kind (single-owner + demote-before-clobber, select-from-
flags on a local, store8-of-flags) remains available but is now low-ROI and high-risk (the
branch-merge machinery — `convergeEdgeTo`'s xor-zeroing, `flushBelow`'s condensing — clobbers
EFLAGS between an early CMP and its Jcc; a pinned flags-local desyncs across the merge). Not
recommended without a new payoff signal. (Plan P3.)

### R2. Float lowering parity — remaining half  · M · 🟦
~~min/max branchy lowering~~ (done #97, with deferred float loads; VEX 3-op #79).
Still open: in-place XMM accumulation (int path has it), float pinned locals still on
the eager call-spill model, float `call; local.set` fusion (int-only today), mixed-call
parallel staging (`emitMixedRegisterCall` still does full `flush()` + slot staging).
(Plan P5.1–.2.)

### R3. Store-narrowing peephole  · S · ✅
Integer comparisons consumed immediately by `i32.store8` now remain in flags and
emit `setcc` directly into a scratch low byte, omitting the dead `movzx`. The rule
selects 173 explicit-mode and 165 guard-mode sites across 11 corpus modules,
removing 684 and 569 native bytes respectively. On a pinned Ryzen 7 7800X3D,
sieve improves from 81.15 to 81.01 µs/op (-0.17%, p=0.000, n=12), while SQLite
and Ruby compilation remain flat at 1.0001x geomean with effectively unchanged
allocation volume. `WAGO_NO_STORE8_FLAGS=1` is the differential oracle. (Plan P2.5/P3.)

### R4. json serialize gap — ✅ RESOLVED (2026-07-02)
Closed by #99 + #100: guard-mode ser 190→**93ns (beats WARP's 97)**; deser 175ns
(1.07× WARP). The forensic trail (B1 `stGlobReg` #93, B2 immediate stores #94, the B3
WARP wat-27 burst diff, and the K-sweep) is preserved in PR #95's findings.
The punchline for posterity: the
bottleneck was never call overhead or global register residency — **~89% of serializer
samples were one backward `std; rep movsb`** in `memory.copy` (no ERMSB/FSRM on
backward copies) on copies that were disjoint and forward-safe. B1+B2 remain as real
codegen improvements (wago's burst emits fewer instructions than WARP's), and the
V0 measurement discipline that found this is now doctrine: profile before chasing a
hypothesis (memory `wago-serialize-memcopy-win`; ≤0x18 perf bins). Serialize is now
flat/GC-bound; no further wago-side lever identified.

### R5. Runtime / infrastructure status (original plan P8)
| Item | Status | Notes |
|---|:--:|---|
| Sync host calls with return values | ✅ done | Synchronous parked-host dispatch supports results. |
| WASI Preview 1 | plugin-owned | `wago-org/wasi` owns the product integration. |
| Invocation cancellation | ✅ done | Native asynchronous cancellation on Linux amd64/arm64; cooperative safepoints elsewhere. |
| Wasm-level trap source frames | 🚧 partial | Function and exact single-site Wasm PCs land; full caller-chain metadata remains follow-up work. |
| Debug mode + bytecode→machine map | [ ] planned | No current product commitment. |
| Arm64 backend | ✅ done | Native runtime paths, CI, and release assets cover all six supported OS/architecture targets. |

### R6. Measurement hardening → **promoted to R0**, see above.

### R7. Adopted from the 2026-07-03 external review (new items; plan §2)
Codegen, cheap-and-safe first: **alias-aware pending loads** (any store currently
flushes ALL deferred loads — keep same-base provably-disjoint ones, plan P2.1) ·
**pure-tree `drop` discard** (P2.2) · ~~**const-fold pack** — compares/eqz/clz/ctz/
popcnt/extensions (P2.3)~~ ✅ DONE (including bounded narrow-load/shift mask elision) · ~~**same-operand
int compare identities** (P2.4)~~ ✅ DONE. Then: **limited multi-result register ABI** (RAX,RDX / XMM0,XMM1 —
unblocks multi-value, with `regMerge2`, P5.3) · **straight-line bounds facts**
(P6.1; the measured hybrid loop-precheck experiment was later removed) · **store
combining** (explicit-only, cold-path sequential replay for trap semantics, P6.3) ·
**CPUID probe** (JIT'd stub, zero deps) gating **BMI2 shifts** + `smallBulkMax`
tuning (P6.5) · **immutable-global const folding** for locals; imported-global
specialization would need guarded dispatch metadata rather than whole-module
recompilation · **`call_indirect` inline
caches** behind a table epoch (P8.6) · **`.wago` cache keys + CLI**
(compile/run/inspect, P8.7) · **call-surviving valent trees** and a **tiny bytecode
inliner** (both decision-gated on R0 counters, P5.4–.5) · **fused validate+compile**
(premise re-measured post-#96, P7). Rejected (with reasons — plan §1.3): `stAddrExpr`,
persistent/general known-bits state, general pending sets with owned regs, tiny unroll, SIMD copy/fill
now, `memory.size` micro-opt.

### Future feature areas (not in WARP either)
SIMD/v128, threads & atomics, exception handling, tail calls, full reference types +
`table.*`, passive element execution, remaining bulk-memory table ops, memory64,
multi-memory. (Cross-instance linking + imported memory/table/global landed
in #112–#115; the `linking`/`data` spec files now pass.)

---

## Architecture decision: no IR (revised 2026-07-03)

**No IR on any execution path — railshot is the only backend.** The earlier "Tier 2
optional SSA" framing is retired; the E-gate SSA-spike question in the perf plans is
answered: no.

- **The pipeline is the identity**: `decode → validate (byte-backed) → scanBody hints
  (summary facts only) → railshot single-pass codegen → native`. Fast validated bytes →
  direct native code; no AST, no SSA, no whole-function IR on the hot path.
- **The ceiling gets attacked incrementally** ("Tier 1.5"): flags-resident values,
  restricted pending sets, call-surviving trees, alias-aware load windows, bounds
  facts — each a small extension of the valent-block storage model, each individually
  gated and measured. The original case for SSA (wazero's json
  edge = its register allocator) has weakened: wago now beats wazero on both json
  directions and most of the corpus without it.
- **`src/core/compiler/ir` stays off-path** as a research/debug package (potential
  differential oracle); it is not a planned tier, not deleted, and not grown.
- **Guardrail**: `scanBody` stays summary-only (scores, shape flags). If it starts
  storing instruction graphs, it has become IR in a trench coat — reject in review.

wago's identity is **low-latency compile**: the single-pass tier is informed,
flush-light, and register-resident, and it stays single-pass.
