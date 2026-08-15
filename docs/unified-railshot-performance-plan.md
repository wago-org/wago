# Unified Railshot performance plan

## Scope

This plan treats the **generated-code-size reduction work as complete** and part of the baseline. Branch relaxation, compaction, objective-aware alignment, byte attribution, and related finalization work are not repeated here.

The completed finalizer remains useful as the last stage of the performance pipeline, but it should stay responsible for:

* Layout.
* Branch/fixup resolution.
* Compaction.
* Islands and veneers.
* Native-PC metadata remapping.

It should **not** grow into a semantic optimizer. Semantic and machine-local optimization should happen before finalization.

## Implementation progress

### 2026-08-14 — immutable optimization selection

The issue #399 compiler lease has been removed from production compilation.
Railshot resolves each architecture's complete optimization selection into an
immutable, bounded `[2]uint64` policy before module work begins. AMD64 and ARM64
carry pre-resolved option tokens through module summaries, worker-owned function
state, lowering, and finalization. The obsolete `Apply`/`ApplySnapshot` path that
temporarily installed selections into backend globals has been deleted.

The acceptance evidence includes opposing-policy concurrent compilation tests
on both architectures, race coverage, deterministic generated-byte comparisons,
and the earlier serial/concurrent benchmarks recorded in
`docs/generated-native-code-size-results.md`. Selection resolution itself remains
zero-allocation. Process-default mutation remains only as a compatibility and
low-level test adapter; it affects subsequently resolved policies and is never
installed for the lifetime of a compilation.

A serialized five-sample Darwin/ARM64 `many_funcs` compile check against the
pre-completion commit measured 262,356 ns/op versus 259,674 ns/op (+1.03%), with
allocations unchanged at 342 allocs/op and B/op effectively unchanged at 138.8
KiB. This remains inside the ordinary local-work compile-time gate.

### 2026-08-14 — local-traffic causality ledger, first slice

The shared opt-in per-function ledger now distinguishes parameter-home stores,
declared-local zero stores, ordinary allocator spills/reloads, structured-merge
stores/reloads, and call-preservation stores/reloads on both backends. Counters
are incremented only at exact emitted frame accesses, remain fixed-size, and are
absent from reports when every value is zero. Code-neutrality tests compare
stats-enabled and disabled output bytes.

A serialized five-sample Darwin/ARM64 `many_funcs` compile check measured a
261,932 ns/op median versus 262,813 ns/op before the ledger (-0.34%, treated as
noise), with 342 allocs/op and approximately 138.8 KiB/op unchanged. This first
slice establishes the baseline for definite assignment, lazy parameter homing,
regional residency, and call-effect work; argument/result moves, directory
derivations, branch counts, and register-bank transfers remain to be attributed.

### 2026-08-14 — reusable call-layout scratch, first #316 slice

AMD64 and ARM64 call-boundary lowering now retain the logical operand layout in
owner-local compiler scratch while `flush` canonicalizes the stack. The common
case uses 64 inline one-byte entries per serial compiler or parallel worker, so
small modules add no heap allocation. Wider stacks share one typed overflow
buffer across functions; capacities above 4 KiB are dropped at the next reset
so one pathological function cannot pin its maximum for the rest of a module.

On a focused Darwin/ARM64 module with eight wide tail-call boundaries, a
serialized three-sample, two-second benchmark reduced compile allocations from
95 to 88 per module and B/op from 60,584 to 59,912. Median compile time was
77,086 ns/op versus 76,458 ns/op (+0.82%). The ordinary small tail-call fixture
remained at 41 allocs/op and did not regress. This is the first deliberately
narrow extraction from #316; local/fact tables, effect summaries, matcher
scratch, and relocation ownership remain separate follow-up slices.

### 2026-08-14 — shared sign-extension provenance, first #438 slice

The AMD64 and ARM64 Valent backends now use one shared one-byte `ValueFacts`
vocabulary. Existing upper-zero and boolean facts are joined by explicit
sign-extension facts for 8-, 16-, and 32-bit producers. Signed loads and Wasm
sign-extension operations establish those facts; target emitters consume them
only for a same-machine-type extension, so an i32-to-i64 extension remains a
tested near miss. Facts survive ordinary materialization and spills without a
side table, and the existing 64-byte storage and 112-byte Valent-node size gates
remain unchanged.

The repository corpus records five `sign-ext-elim` hits across
`tests/regressions/fuzzcases/1797c.wasm` and `1797d.wasm`. Their affected
function bodies shrink by 12 and 8 native bytes respectively on Darwin/ARM64;
module alignment absorbs the first module's total-size reduction. A serialized
three-sample, two-second `many_funcs` compile benchmark measured 247,678 ns/op
versus 247,394 ns/op (+0.11%), with 343 allocs/op unchanged. Exact low-bit
widths, nonzero/alignment facts, structured local-fact joins, and additional
target consumers remain follow-up #438 work.

### 2026-08-14 — immutable integer globals as constants

Defined immutable i32/i64 globals whose initializer is exactly one literal are
now summarized once per module and lowered as ordinary Valent constants on both
backends. The summary is sparse and its single slice header lives in serial or
worker-owned compiler scratch rather than in every `funcHints`; the ARM64
`funcHints` size therefore remains 200 bytes. Modules with no admitted globals
allocate no summary storage. Imported globals, mutable globals, references,
floats, type-mismatched initializers, and extended constant expressions remain
conservative near misses.

The repository corpus records 28 `immutable-global-fold` hits across 16 of
1,333 checked-in Wasm modules. Every hit module shrinks on Darwin/ARM64: 404
native bytes in total, with individual module reductions from 4 to 52 bytes.
Positive i32/i64 cases execute through the native ARM64 harness, and a mutable
global is a tested codegen near miss. A serialized three-sample, two-second
`many_funcs` compile benchmark measured a 249,793 ns/op median versus 252,596
ns/op at the preceding commit (-1.11%, treated as noise), with 343 allocs/op
unchanged and B/op effectively unchanged near 138.9 KiB.

The accompanying rematerialization audit found that Railshot already retains
constants, deferred local/global reads, and deferred addresses symbolically
until their first sink. A new general recipe layer is therefore deferred until
spill attribution identifies a value that is currently materialized and stored
despite being cheaper and semantically safe to reconstruct.

### 2026-08-14 — entry-overwritten parameter homes

The existing one-pass entry-prefix proof now applies to unpinned parameters as
well as declared-local zeroing. When a parameter's first access is a
`local.set` or `local.tee` before any structured-control edge, AMD64 and ARM64
skip its canonical frame-home store. Reads before the write remain a tested
near miss. The proof stays bounded to the existing 64-local mask and is disabled
by the established EH and exact GC-root-plan gates; it adds no summary storage,
body scan, retry, or execution allocation.

The repository corpus records 22 `entry-param-home-elide` hits: 20 in
`bench/corpus/ruby.wasm` and two in `bench/corpus/lua.wasm`. Ruby shrinks by 80
native bytes on Darwin/ARM64 with unchanged spill high-water marks; Lua's two
removed stores are absorbed by existing module alignment. A serialized
three-sample, two-second `many_funcs` compile benchmark measured a 252,713 ns/op
median versus 252,897 ns/op at the preceding commit (-0.07%, treated as noise),
with 343 allocs/op and approximately 138.9 KiB/op unchanged.

### 2026-08-14 — never-read local initialization

The same one-pass local-access scan now records a temporary 64-bit `local.get`
mask. At the end of the scan, locals with no reads are marked as having an
unobservable initial value. This removes parameter-home stores and declared-local
zeroing even when no entry-prefix write exists. The mask lives only in the stack
scanner, so `funcHints` remains 200 bytes on ARM64 and ordinary compilation adds
no allocation. AST-only functions and locals beyond index 63 keep the established
eager fallback; EH and exact GC-root plans continue to clear the optimization.

Across 1,333 checked-in Wasm modules, the broader proof records 2,440 parameter
home eliminations in 24 modules, up from 22 at the preceding commit, plus 170
additional declared-local initialization eliminations. Every affected module
shrinks on Darwin/ARM64, by 11,256 native bytes in total; the largest reduction
is 5,792 bytes in `bench/corpus/script.wasm`. A serialized three-sample,
two-second `many_funcs` compile benchmark measured a 249,592 ns/op median versus
251,990 ns/op (-0.95%), with 343 allocs/op and B/op unchanged.

### 2026-08-14 — bounded overwrite-aware regional eviction

Regional integer residency now copies the active forward Wasm reader and scans
at most 64 operations when it must release a register. If an active local's next
access is an overwrite rather than a read, that local is preferred and its dirty
old value is not stored to the frame. Any structured-control boundary, malformed
immediate, exhausted fuel, pending memory-address borrow, or pending Valent read
falls back to the existing score-based store-and-evict path. The active reader is
stored inline in reusable function state; a rejected pointer-based prototype was
not retained because escape analysis raised `many_funcs` from 343 to 644
allocations/op.

The repository corpus records 159 `interval-region-dead-evict` hits across
`ruby.wasm`, `blake3sum.wasm`, `blake-as.wasm`, and `blake-as-simd.wasm`.
Darwin/ARM64 native code shrinks by 336 bytes total (320 in Ruby and 16 in
blake3sum); the other module totals are alignment-neutral, and spill high-water
marks are unchanged. Five-second execution medians measured blake-as at 397,135
ns/op versus 396,693 ns/op (+0.11%) and blake-as-simd at 413,469 ns/op versus
415,788 ns/op (-0.56%), both with zero B/op and allocations. The standard
`many_funcs` compile median was 248,779 ns/op versus 249,127 ns/op (-0.14%),
with 343 allocs/op and B/op unchanged.

### 2026-08-14 — bounded ARM64 loop-summary ownership

ARM64 loop residency and bounds-hoist admission no longer allocate one map or
scan an unbounded body at every reachable loop header. Exact set-local facts now
occupy a 64-entry, 512-byte arena owned by the serial compiler or parallel
worker; each entry also holds saturated local get/set counts for residency
scoring. Nested frames hold only range descriptors and rewind the arena when
they close. The copied reader has independent 1,024-operation and 16 KiB limits,
and large immediate vectors are capped separately. Exhaustion, malformed input,
or structured EH discards partial facts, marks every loop effect conservatively,
and disables both residency and invariance proofs for that loop.

Cap tests cover operation fuel and local-arena exhaustion, reader restoration,
partial-fact removal, and conservative flags. Existing admitted `fib_iter` and
`sieve` functions produce byte-for-byte equivalent code and identical explain
counters. Five-sample Darwin/ARM64 compile medians measured `fib_iter` at 7,405
ns/op versus 7,334 ns/op (+0.97%), while allocations fell from 37 to 35 and B/op
fell from 17,375 to 17,240. `sieve` measured 12,834 ns/op versus 13,651 ns/op
(-5.98%), with allocations falling from 52 to 44 and B/op from 31,568 to 31,056.
A rejected 256-entry version was not retained because its 1 KiB fixed arena
raised small-module compile B/op by roughly 6.6%.

### 2026-08-14 — measured ARM64 loop-residency scoring

The opt-in ARM64 loop region now chooses at most two integer locals by saturated
static get+set count, with stable local-index tie-breaking, instead of choosing
the first set locals. Admission is limited to functions with at most eight
locals; a measured 10-local `json-as-simd.deserializeN` case regressed by roughly
7% when X12/X13 were reserved despite unchanged spill counts. Explain mode now
attributes pin count, entry loads, branch-edge stores, exit stores, and score
buckets, while normal compilation retains the nil-stats path.

The narrower policy preserves the high-value `utf-as-simd` result: serialized
Darwin/ARM64 medians move from 57,212 to 42,944 ns/op for `convertN` (-24.9%) and
from 142,320 to 19,846 ns/op for `validateN` (-86.1%). Both return the same
results with the policy disabled and enabled (`710400` and `200`) and remain at
zero B/op and allocations. Comparable `json-as-simd` medians improve by 1.0%
for serialize and 0.9% for deserialize. Ordinary `json-as` serialize improves
0.9%, but deserialize remains about 1.5% slower; therefore loop residency stays
opt-in while trip-count/amortization admission is unresolved rather than being
enabled globally on the strength of the UTF result.

With the option disabled, all checked-in benchmark-corpus module sizes remain
identical to the preceding commit. A three-sample `many_funcs` compile median
was 252,370 ns/op versus 248,919 ns/op (+1.39%), with allocations unchanged at
343/op and B/op effectively unchanged near 138.9 KiB.

### 2026-08-14 — bounded ARM64 call-effect summaries, first slice

The existing ARM64 module scan now records compact direct `memory.grow` and
`global.set` effects plus same-module direct-call edges. A shared reverse
worklist computes transitive effects through recursive call cycles in
`O(functions + direct calls)` time per effect bit. The graph is capped at 2,048
functions and 4,096 calls; cap exhaustion and larger modules conservatively
classify every call-making function while retaining exact leaf effects. Tests
exercise recursive propagation, malformed edges, both caps, and the conservative
fallback. Imported, indirect, reference, AST-only, and custom-instruction calls
remain fully conservative.

ARM64 register-ABI direct calls use the summary to retain an existing explicit
linear-memory bounds certificate when the callee transitively cannot grow
memory. Certificates derived from mutable globals additionally require a
no-global-write proof, and a call result targeting the source local remains a
near miss. The optimization has an immutable per-compilation
`call-effect-bounds` option and `WAGO_ARM64_NO_CALL_EFFECT_BOUNDS=1` rollback
switch; guard mode and modules without memory allocate no effect state.

The benchmark corpus records 2,168 preserved certificates across 19 modules.
Darwin/ARM64 native code falls by 5,120 bytes in total; alignment absorbs some
individual removals. The large-graph fallback kept SQLite compile overhead to
about 13 KiB/op (+0.26%) and two allocations, versus a rejected unbounded
prototype's roughly 397 KiB/op increase. Median SQLite backend time was 50.12 ms
enabled versus 49.56 ms disabled (+1.1%, treated as noise). Reverse-order
raytrace execution measured 259.8 us/op enabled versus 259.4 us/op disabled
(+0.15%), and the measured JSON/JSON-SIMD rows remained within approximately
1.3%; all retained zero execution allocations.

### 2026-08-14 — finite ARM64 internal ABI classes, first slice

ARM64 direct-call metadata now stores a one-byte finite class rather than a
free-standing preservation boolean. The first two contracts are `General` and
`LeafScalar`. The established call-free integer leaf class remains intact, and
effect-safe memory-touching leaves may now join it when they have no declared
locals, global access, nested calls, or `memory.grow`. The callee reserves the
caller's pin bank and keeps parameters in incoming registers, so the caller can
avoid pinned-local/global preservation and post-call reload work. Every rejected
shape uses `General`; `WAGO_ARM64_NO_ABI_CLASSES=1` and the immutable
per-compilation `abi-classes` option restore the narrower admission policy.

Focused codegen tests prove the memory-leaf class removes the caller's
preservation store/reload pair, and native execution tests compare enabled and
disabled code through the real ARM64 wrapper. The corpus contains 1,544 admitted
memory leaves across 14 modules. Thirteen module images shrink, by 11,184 native
bytes in total, and none grows. A serialized five-sample benchmark with sixteen
calls to one admitted leaf measured 21.68 ns/op versus 22.19 ns/op (-2.3%), with
zero B/op and allocations. `many_funcs` compile B/op and allocations remain
unchanged; median time moved from 248,205 to 248,999 ns/op (+0.32%). SQLite also
kept identical B/op and allocations, with median backend time moving from 51.14
to 50.94 ms (-0.39%).

### 2026-08-14 — shared call effects and AMD64 bounds-state parity

The bounded call-effect collector now lives in `railshot/shared` and both target
scanners feed it during their existing module pass. Each backend keeps the same
2,048-function and 4,096-edge caps, transitive recursive propagation, exact leaf
fallback, and conservative treatment of imported, indirect, reference, AST-only,
and custom-instruction calls. Shared tests own propagation and cap behavior;
target tests prove each scanner's transitive `memory.grow` and `global.set`
classification.

AMD64 direct register-ABI calls now snapshot the fixed eight-entry bounds
certificate set and restore it only when the local callee transitively cannot
grow memory. A fused result drops the overwritten local's certificate, while a
callee that may write globals drops every global-derived certificate and retains
safe local-derived entries. `WAGO_AMD64_NO_CALL_EFFECT_BOUNDS=1` and the shared
immutable `call-effect-bounds` option retain blanket call invalidation.

Native Linux/AMD64 measurements on a Ryzen 7 7800X3D record 3,076 preserved
certificates across 19 corpus modules and 8,736 fewer native bytes. No measured
module grows. A serialized seven-sample benchmark with sixteen safe calls
measured 23.14 ns/op enabled versus 24.01 ns/op disabled (-3.6%), with zero B/op
and allocations. Reverse-order corpus checks were neutral to favorable:
raytrace measured 386.68 us/op versus 389.84 us/op (-0.81%), and JSON serialize
measured 23.68 us/op versus 23.91 us/op (-0.96%). `many_funcs` compile time,
B/op, and allocations were flat; SQLite added approximately 3 KiB/op (+0.05%)
and one median allocation while backend time moved from 82.18 to 82.06 ms.

### 2026-08-14 — bounded register ABI v2 result banks

Both backends now share a fixed four-entry scalar result plan. A register-ABI
signature may return at most two GP values and two FP values, mapped in Wasm
result order to `RAX/RDX` plus `XMM0/XMM1` on AMD64 or `X0/X1` plus `V0/V1` on
ARM64. The plan is returned by value, allocates no storage, and rejects vectors,
references, a third result in either bank, or more than four total results. Every
rejected signature retains the established wrapper/slot ABI.

Direct mixed calls capture GP results before post-call state restoration while
leaving the dedicated FP result bank intact. Internal epilogues and host adapters
use the same plan, so direct calls, exported entry, returns, and the wrapper
fallback agree on result locations. Descriptor-based indirect tails retain their
wrapper tag and restore up to four scalar results through one fixed 64-byte
record; direct tails keep register transfer. Native execution tests cover an
interleaved four-result call, all four adapter result words, direct tail transfer,
mixed indirect-tail restoration, and explicit `reg-abi=false` fallback.

The sixteen-call targeted benchmark measured 26.51 ns/op with register results
versus 40.23 ns/op through ARM64 result buffers (-34.1%), and 32.07 versus 49.81
ns/op on a Ryzen 7 7800X3D (-35.6%); all paths remained zero-allocation. The
target module fell from 888 to 420 ARM64 native bytes and from 993 to 363 AMD64
bytes. Seven-sample compile medians were also neutral to favorable: ARM64 moved
from 6.922 to 6.876 us/op with 33 allocations in both cases, while AMD64 moved
from 9.473 to 9.311 us/op and 36 to 35 allocations.

The current 64-module corpus contains no mixed multi-result call admitted only by
v2. This is therefore a measured signature-specific win, not a current broad-
corpus claim; the opt-in causality counter `regabi-v2-result` will expose future
producer uptake without adding normal-compilation overhead.

### 2026-08-14 — generated ARM64 machine-rule foundation

The existing fixed 24-operation ARM64 call-shuffle window now selects its
three-register swap-chain rewrite through generated plain Go rather than a
handwritten matcher. A checked-in declarative rule is the source of truth; its
generator validates the admitted shape and emits the rule ID, captures, decision
predicate, and explain name. Parsing, formatting, and schema validation exist
only in the generator, so production compilation retains no maps, reflection,
parser, heap-backed matcher state, or general machine IR. Target emission remains
inside ARM64.

The first rule has exhaustive register-state equivalence coverage over its
bounded input domain, individual near misses for operation kind, producer link,
and distinct-output requirements, and a source hash that fails tests when the
generated matcher is stale. Capacity-exhaustion identity coverage remains in
place. An eight-sample same-binary microbenchmark measured a 0.769 ns/op median
for the generated matcher versus 0.770 ns/op for the equivalent direct condition;
both report zero B/op and allocations. This is infrastructure with preserved
code generation, not a new execution-speed claim. Further rule families remain
gated on provenance, effects, physical constraints, corpus hits, and A/B results.

### 2026-08-14 — bounded ARM64 LeafFP internal ABI class

ARM64's finite internal ABI classes now include `LeafFP` for scalar signatures
that use either register bank. Admission remains deliberately narrow: the callee
must fit the register ABI, have no declared locals, calls, globals, or control
flow, have at least one direct local callsite, and have at most 12 estimated
operand-stack nodes. The callsite requirement prevents exports and otherwise
uncalled private functions from reserving pin banks without removing any caller
traffic. Memory-touching leaves
still require the existing effect proof and may not grow memory. Every rejected
shape uses `General`, and `abi-leaf-fp` is an independent immutable policy bit
with `WAGO_ARM64_NO_ABI_LEAF_FP=1` as its exact A/B switch.

An admitted callee keeps parameters in `X0..X7` and `V0..V7`, reserves the full
caller GP and FP pin banks, and disables function-local FP/vector constant caches
that would compete for that reduced scratch set. Mixed direct calls can therefore
leave dirty caller locals in registers instead of storing and lazily reloading
them. A pressure test initially found the wider 32-node proposal could exhaust
the restricted V-register set; the production cap was reduced to 12 and that
shape now falls back conservatively.

The 64-module corpus admits 17 LeafFP functions across Ruby, Script, and SQLite.
All three affected module images shrink, by 576 native bytes in total, and no
module grows. A four-live-FP-local benchmark making 64 direct
calls measured 50.46 ns/op versus 51.76 ns/op for `General` (-2.5%), with zero
B/op and allocations; native code fell from 2,552 to 1,516 bytes. Seven-sample
focused compile medians improved from 38.85 to 36.20 us/op with 49 allocations
unchanged. Five-sample SQLite backend medians were neutral (52.72 ms enabled
versus 52.68 ms disabled), with 25,132 allocations unchanged and B/op noise-only.

### 2026-08-14 — fixed AMD64/ARM64 prepared FP entry family

Prepared invocation now has one finite FP-only entry family for up to four
`f32`/`f64` parameters and at most one FP result. One static foreign-stack
trampoline per supported architecture transfers raw IEEE-754 bits between Go
argument slots and `XMM0..XMM3` or `V0..V3`, enters the existing register-ABI
internal entry, and returns `XMM0`/`V0` without a serialized argument/result
buffer round trip. It is admitted only for isolated private instances and the
compiler's existing bounded direct-entry functions; mixed signatures,
references, vectors, wider arities, shared execution control, and every
unsupported architecture retain the ordinary prepared path.

`prepared-fp-entry` is an immutable compiler policy bit with
`WAGO_AMD64_NO_PREPARED_FP_ENTRY=1` and
`WAGO_ARM64_NO_PREPARED_FP_ENTRY=1` as metadata rollbacks. Runtime selection has
an independent `WAGO_PREPARED_DIRECT_FP=0` fallback. Both `f32` and `f64`
bit-preserving execution and both fallback layers are covered natively on both
architectures.

Five-sample medians for a prepared `f64.add` improved from 30.53 ns/op to
18.77 ns/op (-38.5%) on an Apple M4 Max and from 15.59 ns/op to 7.44 ns/op
(-52.3%) on an AMD Ryzen 7 7800X3D, with zero B/op and allocations on every
path. Focused compile medians moved from 9.93 to 10.05 us/op (+1.2%) on ARM64
and from 17.32 to 17.41 us/op (+0.5%) on AMD64. Compile storage rose by 16 bytes
(0.10% ARM64, 0.13% AMD64) and one allocation for the persistent per-module
direct-entry bitset. Generated function bytes are unchanged because the compiler
change is metadata-only.

### 2026-08-14 — fixed mixed-bank prepared entry family

The same prepared-entry policy now admits one mixed numeric family capped at two
GP plus two FP arguments, four total, and at most one scalar result. Public raw
slots are compacted into the two physical argument banks with a fixed unrolled
upper bound. One static trampoline per architecture returns both physical result
banks; the prepared signature selects the declared bank in Go. No dynamic
trampoline, signature cache, heap allocation, or native result-kind branch is
introduced. Pure GP and pure FP signatures retain their smaller trampolines.

Mixed `f32`/`f64` and `i32`/`i64` ordering, dirty upper-bit cleanup, both result
banks, division traps, post-trap recovery, cap exhaustion, nonnumeric types, and
multi-result fallback are covered. The family remains isolated-instance-only and
shares `prepared-fp-entry` / `WAGO_PREPARED_DIRECT_FP=0` rollback with the FP
family.

Five-sample medians for a four-argument mixed function improved from 32.13 to
20.88 ns/op (-35.0%) on an Apple M4 Max and from 18.03 to 10.67 ns/op (-40.8%)
on an AMD Ryzen 7 7800X3D. Every path remains at zero B/op and allocations.
Compile storage and generated function bytes are unchanged beyond the already
measured direct-entry metadata bitset.

### 2026-08-14 — ARM64 halfword ZIP shuffle selection

Exact `i8x16.shuffle` masks for halfword `ZIP1` and `ZIP2` now lower to one
native NEON instruction instead of two table lookups, two mask constants, and
an OR. The selector is a pair of fixed-array comparisons in the existing
one-operation shuffle path. It adds no scan, retained IR, dynamic storage, or
retry, and `shuffle-half-zip` is an immutable per-compilation policy bit with
`WAGO_ARM64_NO_SHUFFLE_HALF_ZIP=1` as the process-default rollback.

Both positive masks execute through the native ARM64 harness, including pinned
destination aliasing. Disabling the rule and changing one mask lane are tested
fallbacks. A scan of the top-level checked-in corpus found exactly two hits,
both in `utf-as-simd.wasm`; native code falls from 20,444 to 20,252 bytes
(-192 bytes, -0.94%). Four alternating two-second `validateN` comparisons were
timing-neutral (medians of 142.12 us enabled and
142.09 us disabled), with zero B/op and allocations. Five one-second full
compile samples measured 558.0 us/op enabled versus 561.1 us/op disabled
(-0.55%, treated as noise), with 362 allocations/op unchanged and effectively
unchanged B/op near 309.9 KiB.

### 2026-08-14 — bounded mixed-bank prepared results

The fixed mixed-bank prepared trampoline now carries exactly one GP and one FP
result back to Go in either declared order. Admission remains finite: at most
four scalar parameters, at most two parameters per register bank, and exactly
one result per bank for the two-result form. It reuses the existing static
trampoline, compiler register ABI, result storage, and `prepared-fp-entry` /
`WAGO_PREPARED_DIRECT_FP=0` rollback; no dynamic trampoline, signature cache,
allocation, or additional native entry code is introduced.

Tests cover GP-first and FP-first results, pure and mixed parameter banks,
no-argument admission, 32-bit upper-bit cleanup, same-bank and nonnumeric near
misses, compiler and runtime rollback, division traps, and post-trap recovery.
The focused suite passes natively on ARM64 and through Darwin's AMD64 execution
path. Three two-second Apple M4 Max samples improved the prepared pass-through
median from 82.29 to 19.30 ns/op (-76.5%), with zero B/op and allocations. Five
one-second `many_funcs` full-compile samples measured 391.1 us/op enabled versus
391.8 us/op disabled (-0.17%, treated as noise), with 381 allocations/op,
approximately 200.3 KiB/op, and 28,968 generated code bytes unchanged.

### 2026-08-14 — bounded same-bank prepared results

Prepared integer and FP entries now return at most two results from one register
bank through fixed architecture trampolines: `RAX`/`RDX` and `XMM0`/`XMM1` on
AMD64, or `X0`/`X1` and `V0`/`V1` on ARM64. Existing one-result trampolines are
unchanged. Admission remains limited to four scalar parameters and two scalar
results; mixed-bank results keep their prior trampoline, while larger, reference,
and vector signatures keep their established fallbacks. The trampolines reuse
the instance's two-slot result buffer and add no signature cache, generated
adapter, unbounded state, or execution allocation.

Tests cover both banks, result order, i32/f32 upper-bit cleanup, i64 parameter
preservation, compiler and runtime rollback, a three-result near miss, division
traps, and post-trap recovery. The focused suite passes natively on both
architectures. Three one-second Apple M4 Max samples improved integer pairs from
a median 83.88 to 19.00 ns/op (-77.3%) and FP pairs from 83.59 to 19.37 ns/op
(-76.8%). Five one-second Ryzen 7 7800X3D samples improved integer pairs from
87.88 to 9.381 ns/op (-89.3%) and FP pairs from 87.22 to 9.693 ns/op (-88.9%).
Every measured path remains at zero B/op and allocations. The existing
one-result FP direct path remains at roughly 18.0 ns/op on the Apple system. A
five-sample `many_funcs` compile run measured a 263.3 us/op median with 344
allocations/op; the implementation adds no compiler scratch or generated
function bytes beyond the existing direct-entry selection.

### 2026-08-14 — structured definite-assignment entry elision

The combined byte-body scan now carries two inline 64-bit masks for definite
writes and reads of an initial local value. Straight-line writes dominate later
reads, and simple `if` arms retain only the intersection of definitely written
locals. Blocks, loops, `try_table`, escaping control edges, AST-only bodies,
locals beyond index 63, and conservative GC-root plans keep the established
eager initialization fallback. The result removes both declared-local zero
stores and parameter homes when the incoming value cannot be observed, without
a CFG, retained data-flow table, heap allocation, or second semantic traversal.
The option is part of the immutable per-compilation policy and has an explicit
rollback path.

Across the 1,053 compilable modules among 1,333 checked-in Wasm files, the
broader proof removes 339 parameter-home stores and 30,888 declared-local zero
stores. Total Darwin/ARM64 native output falls by 137,764 bytes; Ruby alone
shrinks by 58,640 bytes, with Script, SQLite,
Esbuild, Regexmatch, and Lua also contributing materially. Exact alternating
Ruby compile measurements produced a +1.29% median time delta, while B/op fell
by about 56 KiB and allocations were effectively unchanged. Alternating `json-as`
execution medians were effectively flat for serialize (+0.26%) and improved
1.72% for deserialize, with zero B/op and allocations throughout.

Positive execution tests cover both outcomes of a definitely assigning `if`;
single-arm assignment, an escaping `br_if`, block/loop conservatism, the
64-local cap, and policy-disabled compilation are tested near misses. The scan
frame grows by 32 bytes per recursive control level; at the existing 20,000
level guard the static upper-bound delta is 640 KiB, below the plan's 1 MiB
absolute gate. Full repository and focused race tests pass. Native AMD64
correctness passes on the Ryzen 7 7800X3D host. Six one-second Ruby backend
compile samples move from a 926.29 ms disabled median to 928.70 ms enabled
(+0.26%); B/op and allocation counts are effectively unchanged. Order-reversed
JSON runs improve serialize from about 110.5 to 108.3 ns/op (-2.0%) and
deserialize from 197.1 to 194.1 ns/op (-1.5%), with zero B/op and allocations.

### 2026-08-14 — retained immediate dead constructors on ARM64

ARM64 now recognizes an exact one-byte consumer window for `struct.new`,
`struct.new_default`, `array.new`, `array.new_default`, `array.new_fixed`, and
`array.new_data` whose result is immediately dropped. Reference-uniform and
element-segment arrays remain on the full constructor path.
The lowering still evaluates every operand, performs the real collector
allocation, preserves allocation and operand traps, and records the ordinary GC
safepoint. It replaces unreachable payload population with the existing shared
dead-reservation helpers, then consumes the dead operand trees. Observable
results and a per-compilation `gc-dead-new=false` selection keep the full
constructor path. A copied reader also recognizes at most 32 postfix operations
in pointer-safe nested constructor trees. Each nested reservation retains its
real compact result as a root across later allocations; reference-containing
intermediates keep the full constructor path. Fuel exhaustion and unknown
operations fall back without partial transformation or reader movement.

The five-constructor native fixture falls from 620 to 548 function bytes, while
allocation-family attribution falls from 968 to 824 bytes. Five alternating
Apple M4 Max compile samples improved from a 6.998 to 6.662 us/op median
(-4.8%), with allocations falling from 37 to 34 and median B/op falling by
125 bytes. With a nested numeric-array/struct tree included, five alternating
sustained product samples improved from 722.1 to 609.0 ns/op (-15.7%), retaining
zero B/op and allocations. Forced collection executes 700 retained allocations
correctly. A division trap in a constructor
operand and an out-of-bounds passive-data range are verified before allocation;
the subsequent nontrapping call proves recovery and exact retained allocation
counting. The shared
optimization catalog now gives AMD64's existing broader implementation the same
immutable per-compilation rollback.

### 2026-08-14 — ARM64 constructor-known array lengths

ARM64 now recognizes an exact adjacent array constructor to `array.len` shape
through a copied Wasm reader. `array.new_fixed` contributes its immediate count;
`array.new`, `array.new_default`, and `array.new_data` contribute
only when their existing Valent length operand is an exact i32 constant. The
constructor still evaluates every initializer, performs segment bounds checks,
runs the ordinary allocating helper and safepoint, and produces a rooted
reference. Only the immediately consumed reference and second helper transition
are replaced by the proven constructor count. Other GC operations, dynamic
lengths, malformed suffixes, intervening operations, oversized helper payloads,
and an immutable per-compilation `gc-fixed-array-len=false` selection retain the
ordinary path. The matcher has no retained state or allocation and commits reader
movement only for the exact producer/consumer shape.

The focused function falls from 300 to 244 native bytes and from two synchronous
GC helper transitions to one. Five alternating Apple M4 Max execution samples
improved from a 288.1 to 252.7 ns/op median (-12.3%) with zero B/op and
allocations. Five alternating compile samples improved from 4.728 to 4.379
us/op (-7.4%); allocations remained 31/op and median B/op was effectively flat.
Both enabled and disabled product paths return the encoded length and retain
exactly 100 collector allocations across 100 forced-collection invocations.
Copied-reader match, near-miss, truncated-immediate, policy-disabled, helper-call,
native-byte, and collector-verification tests cover the bounded fallback.

The three-site uniform/default/data fixture further falls from 600 to 436 native
bytes and from six synchronous GC helpers to three. Five alternating execution
samples improved from a 531.8 to 411.5 ns/op median (-22.6%), still with zero
B/op and allocations. Five alternating compile samples improved from 7.065 to
6.465 us/op (-8.5%); allocations remained 35/op and median B/op changed by only
7 bytes. Enabled and disabled forced-collection paths both retain exactly 300
allocations across 100 calls. A mixed fixture proves one dynamic length keeps its
own `array.len` helper while independent constant-length sites still fuse.

### 2026-08-14 — ARM64 constructor-known numeric struct reads

ARM64 now recognizes exact adjacent `struct.new` or `struct.new_default` to
`struct.get`, `struct.get_s`, or `struct.get_u` shapes when the selected numeric
initializer is already a Valent constant. A copied reader requires the same
defined type and valid field/access form. Plain i32/i64 constants and packed
i8/i16 signed or unsigned reads are normalized at compile time. Float, vector,
reference, dynamic, mismatched-type, non-adjacent, and malformed cases retain
the existing helper path. The constructor still evaluates every field, performs
the real allocation and safepoint, and roots its result; only the successful
adjacent read helper is replaced. The immutable `gc-const-struct-get` policy
provides an exact rollback.

The three-site plain/packed/default fixture falls from 672 to 408 native bytes
and from six synchronous GC helpers to three. Five alternating Apple M4 Max
execution samples improved from a 498.8 to 368.2 ns/op median (-26.2%) with zero
B/op and allocations. Five alternating compile samples improved from 7.547 to
6.301 us/op (-16.5%); allocations remained 35/op and median B/op rose by 89
bytes (+0.54%), within the gate. Enabled and disabled forced-collection paths
both retain exactly 301 successful allocations across 100 main calls and one
nontrapping operand call. A division by zero in an unselected initializer traps
before allocation, while the subsequent nontrapping call proves recovery. A
dynamic selected-field initializer is an explicit near miss and retains both
helpers.

### 2026-08-14 — ARM64 exact constructor-cast elision

ARM64 now recognizes an exact adjacent `struct.new`/`struct.new_default` or
bounded array constructor followed by `ref.cast` or `ref.cast_null` to the same
defined type. Constructor results already prove that exact non-null type, so the
cast is an identity. The allocating helper, initializer evaluation, safepoint,
rooted result, OOM/segment traps, and following consumers remain unchanged; only
the redundant cast helper disappears. A copied reader admits only the exact
same-type producer/consumer pair. Other targets, malformed suffixes, intervening
operations, oversized vector constructors, dropped-constructor trees, and the
immutable `gc-constructor-cast=false` policy retain the ordinary path.

The struct/array fixture falls from 504 to 296 native bytes and from four
synchronous GC helpers to two. Five alternating Apple M4 Max execution samples
improved from a 382.5 to 299.6 ns/op median (-21.7%) with zero B/op and
allocations. Five alternating compile samples improved from 6.604 to 4.751
us/op (-28.1%); allocations fell from 35 to 31 and median B/op fell by about
4.2 KiB. Enabled and disabled forced-collection paths both retain exactly 200
allocations across 100 calls, and a non-adjacent cast is an explicit near miss.

### 2026-08-14 — ARM64 wide-bitmask consumer fusion

ARM64 now recognizes exact adjacent `i16x8.bitmask`/`i32x4.bitmask` consumers
that either compare the mask with zero or population-count it, plus
`i64x2.bitmask` followed by population count. The lowering shifts each lane's
sign bit to 0/1 and horizontally sums those values, avoiding the lane-weight
constant and packed scalar-mask synthesis. It uses at most two copied-reader
operations, has no retained state or allocation, is controlled by immutable
per-compilation policy, and leaves the live reader untouched on every near miss.
Comparisons with nonzero constants retain the ordinary lowering. The two-lane nonzero form
was deliberately excluded after an Apple M4 Max trial measured it slower.

Five Apple M4 Max samples of a 64-sequence native fixture improved medians from
32.36 to 14.46 ns/op for i16x8 nonzero (-55.3%), 35.36 to 11.67 ns/op for
i16x8 popcount (-67.0%), 26.05 to 13.82 ns/op for i32x4 nonzero (-47.0%),
31.70 to 11.84 ns/op for i32x4 popcount (-62.6%), and 27.13 to 19.13 ns/op for
i64x2 popcount (-29.5%). Every path remained zero-allocation. Per-site native
bytes fell from 160 to 120, 168 to 112, 132 to 108, 140 to 100, and 104 to 88,
respectively. On the 64-sequence i16x8 nonzero compile fixture, five-sample
median compile time improved from 31.475 to 20.765 us/op (-34.0%), native bytes
fell from 4,808 to 1,988, compile B/op fell from 100,187 to 42,795, and
allocations fell from 23 to 21.

### 2026-08-14 — AMD64 i16x8 bitmask zero-test fusion

AMD64 now shares the immutable `simd-wide-bitmask-consumer` policy and recognizes
the exact adjacent `i16x8.bitmask; i32.const 0; i32.ne` consumer. Because only
zero-ness is observable, it arithmetic-shifts each word's sign across that word
and tests the byte movemask directly. This removes the saturating pack and scalar
mask operation without changing the baseline ISA. A copied reader examines two
Wasm operations, so nonzero comparisons, popcount, malformed suffixes, disabled
policy, and every non-adjacent consumer retain the ordinary path. Direct
`i32x4`/`i64x2` movemask instructions already make their zero-test paths compact,
so this slice does not replace them speculatively.

Five serial samples on a Ryzen 7 7800X3D of a 64-sequence native fixture improved
the median from 31.96 to 29.90 ns/op (-6.4%) with zero B/op and allocations, while
native function bytes fell from 2,396 to 1,977. The corresponding compile fixture,
including code-image release, improved from 36.348 to 29.854 us/op (-17.9%);
compile B/op fell from 95,562 to 38,170, allocations fell from 19 to 17, and
native bytes fell by the same 419 bytes. Single-site fixtures save two final
aligned bytes.

### 2026-08-14 — ARM64 native final-cast array length

ARM64 now lowers an exact adjacent cast to a final array type followed by
`array.len` through the versioned collector native view. The bounded inline path
reloads mutable handle and heap backing, validates the compact handle, space,
range, header extent, and exact canonical type, then reads the array length
without a second synchronous Go transition. Null, `ref.cast_null`, i31, and cast
failure ordering match the helper path. One immutable per-compilation option
controls the lowering; disabled, Size, and Embedded policies retain the helper.

The common cast/null trap-site storage now uses 144 owner-local inline bytes per
compiler worker, eliminating the native path's transient slice growth while
retaining bounded slice fallback for pathological functions. Five Apple M4 Max
execution samples improved from a 322.6 to 285.6 ns/op median (-11.5%) with zero
B/op and allocations. The deliberately tiny compile fixture increased from
4.969 to 5.747 us/op (+15.7%), while median B/op rose 0.8%, allocations fell
from 36 to 35, and Balanced native output grew from 316 to 440 bytes. The
speed-oriented Balanced path accepts that local tradeoff to remove a runtime
helper; compact objectives reject it, and the growth remains visible in native
byte attribution.

### 2026-08-14 — ARM64 native final scalar struct reads

The checked ARM64 exact-final resolver now returns a bounded, ephemeral object
address to one immediate consumer. Adjacent final casts followed by scalar
`struct.get`, `struct.get_s`, or `struct.get_u` use that shared resolver and load
packed i8/i16, i32/i64, or f32/f64 fields directly. Ordinary scalar reads whose
validated operand is already a nullable reference to the same final type use the
same path without cast lookahead. Every mutable native-view
pointer is reloaded after the constructor safepoint; the handle, heap range,
required field extent, and exact canonical type are checked before the load.
Reference and vector fields, missing layout metadata, disabled policy, and
Size/Embedded objectives retain the combined helper.

Native execution covers all six admitted storage classes, signed and unsigned
packed extension, null/cast/i31 trap order, and 100 forced major collections
with collector verification. Five Apple M4 Max i32 samples improved from a
324.1 to 280.1 ns/op median (-13.6%) with zero B/op and allocations. The tiny
compile fixture increased from 5.740 to 6.072 us/op (+5.8%) and from 348 to 448
native bytes, while B/op fell 25.2% (16,596 to 12,416) and allocations fell from
40 to 35. Compact objectives keep the helper to avoid that local code growth.
The separate ordinary `struct.get` fixture improved from 317.6 to 274.9 ns/op
(-13.4%). Its tiny compile fixture increased from 4.774 to 5.519 us/op and from
316 to 496 native bytes, while B/op fell 0.9% and allocations fell from 36 to
34. Both execution paths remain zero-allocation.

### 2026-08-14 — bounded ARM64 native GC resolution reuse

ARM64 now retains one checked raw object address across an adjacent run of
scalar reads from the same local, including reads fused through repeated exact
final casts. The compact reference remains the rooted
semantic identity. The raw address occupies one protected register and is
discarded at calls, collector safepoints, local mutation, control boundaries,
non-GC operations, and unsupported GC suboperations. The first read validates
the final struct's complete immutable object extent, allowing later fields at
higher offsets to reuse the same certificate without weakening malformed-handle
checks. An immutable per-compilation option and
`WAGO_ARM64_NO_GC_RESOLVE_REUSE=1` retain the independent checked path.

Positive tests reach seven reuses in one eight-read region; a constructor
safepoint near miss proves that reuse stops and may begin again only afterward.
On Apple M4 Max, eight adjacent reads improved from a 99.5 to 87.1 ns/op median
(-12.5%) through prepared invocation with zero B/op and allocations. The
matching compile fixture improved from 11.44 to 8.06 us/op (-29.6%), allocations
fell from 52 to 46, B/op fell 12.8%, and native output fell from
1,888 to 740 bytes because seven duplicate checked resolvers disappeared.
The repeated explicit-final-cast compile fixture likewise improved from a 12.09
to 8.43 us/op median (-30.2%), cut B/op 20.4%, removed three allocations, and
reduced native output from 1,844 to 696 bytes.

Repeated final-cast `array.len` uses the same certificate while loading each
length into a separate result register, so the cached raw address is never
overwritten. Its Apple M4 Max prepared-invocation median improved from 98.7 to
89.8 ns/op (-9.0%) with zero B/op and allocations. The compile fixture improved
from 11.17 to 7.96 us/op (-28.7%), cut B/op 20.5%, removed three allocations,
and reduced native output from 1,764 to 616 bytes.

### 2026-08-14 — ARM64 native final array scalar reads

ARM64 now lowers scalar `array.get`, `array.get_s`, and `array.get_u` for final,
pointer-free element layouts through the checked native object resolver. The
path preserves null-before-bounds trap order, checks the logical index against
the array length, independently checks the scaled element extent against the
object header size, and handles packed i8/i16, i32/i64, and f32/f64 loads.
Reference/vector arrays, non-final layouts, disabled policy, and Size/Embedded
objectives retain the helper. Repeated reads from one local reuse the bounded
raw-address certificate; constant indices do not invalidate it.

Execution tests cover null, logical out-of-bounds, all admitted scalar storage
classes, signed/unsigned extension, and an eight-read reuse region. On Apple M4
Max, that region improved from a 471.4 to 90.0 ns/op median (-80.9%) through
prepared invocation with zero B/op and allocations. Compilation improved from
13.75 to 11.10 us/op (-19.3%), B/op fell 63.4%, and allocations fell from 66 to
54. Balanced native output grew from 1,384 to 1,588 bytes (+14.7%); compact
objectives retain helpers, and the growth stays within the fixture's explicit
256-byte budget.

### 2026-08-14 — ARM64 native final array scalar writes

Final mutable arrays with pointer-free scalar elements now share the native
checked-address path for `array.set`. The value and index remain protected while
the exact object is resolved; logical bounds and physical object extent are
checked before the store. Packed i8/i16, i32/i64, and f32/f64 stores execute
natively, while reference/vector elements, non-final or immutable layouts,
disabled policy, and compact objectives retain the helper. Scalar writes keep a
same-local raw-address certificate valid because they cannot move collector
backing or change object identity.

Execution covers null and bounds traps, every admitted scalar storage class,
and an eight-write region followed by a native read. On Apple M4 Max, that region
improved from a 613.3 to 96.5 ns/op median (-84.3%) through prepared invocation
with zero B/op and allocations. Compilation improved from 11.67 to 9.27 us/op
(-20.5%), B/op fell 35.4%, and native output fell from 1,508 to 1,152 bytes;
compile allocations increased by one (49 to 50) and remain explicitly tracked.

### 2026-08-14 — ARM64 native final struct scalar writes

Final mutable structs with pointer-free scalar fields now lower `struct.set`
through the same checked native object resolver as scalar reads. The value stays
protected while the nullable reference, handle, heap extent, required field
extent, and exact final type are checked, then packed i8/i16, i32/i64, or
f32/f64 data is stored directly. Reference/vector fields, non-final or immutable
layouts, disabled policy, and Size/Embedded objectives retain the helper. An
adjacent run reuses one bounded raw-address certificate because scalar stores
cannot move collector backing or change object identity.

Execution tests cover null traps, all admitted scalar storage classes, signed
and unsigned packed round trips, and eight alternating field writes followed by
a native read. On Apple M4 Max, that region improved from a 554.7 to 89.2 ns/op
median (-83.9%) through prepared invocation with zero B/op and allocations.
Compilation improved from 11.54 to 7.90 us/op (-31.5%), B/op fell 36.5%,
allocations fell from 47 to 41, and native output fell from 1,404 to 728 bytes
because seven duplicate checked resolvers and all eight helper calls disappeared.

### 2026-08-14 — ARM64 native final struct reference reads

Final struct fields that use the collector's compact four-byte reference
representation now load directly after the same nullable-handle, heap-extent,
and exact-final-type checks as scalar fields. The loaded compact reference is
marked as a frame root whenever the function has exact root maps. Function and
external references, non-final layouts, disabled policy, and Size/Embedded
objectives retain the helper. Repeated reads from one local reuse the bounded
raw parent address, while the compact child remains the only semantic value
that may cross a safepoint.

Execution tests keep the final loaded child live across a subsequent allocating
helper and run 100 iterations with collection on every allocation, forced major
collections, and collector verification under both A/B settings. A separate
near miss proves that an eight-byte function-reference field remains helper
bound. On Apple M4 Max, eight reference reads in the admitted constructor
fixture improved from a 655.5 to 324.5 ns/op median (-50.5%) through prepared
invocation with zero B/op and allocations. Compilation improved from 10.84 to
8.25 us/op (-23.8%), B/op fell 34.5%, allocations fell from 42 to 40, and
native output fell from 1,352 to 880 bytes because seven duplicate checked
resolvers and eight read helpers disappeared.

### 2026-08-14 — ARM64 native final array reference reads

Final arrays whose elements use the collector's compact four-byte reference
representation now lower `array.get` through the checked native object view.
The first access validates the nullable handle, exact final type, logical index,
and the immutable array's complete physical extent; later reads from the same
local reuse that bounded raw-address and extent certificate. Loaded compact
references remain exact frame roots across safepoints. Function/external
references, non-final layouts, compact objectives, and disabled policy retain
the helper path.

Execution tests cover a valid read, an out-of-bounds trap, forced collection
with the loaded child live across a subsequent allocation, resolver reuse, and
the function-reference near miss. On Apple M4 Max, eight admitted reads improved
from a 691.5 to 340.8 ns/op median (-50.7%) through prepared invocation with
zero B/op and allocations. Compilation improved from a 10.48 to 9.57 us/op
median (-8.7%), native output fell from 1,320 to 1,156 bytes (-12.4%), and
compile memory increased by only 96 B/op (0.42%) and one allocation (41 to 42).
Trap-site metadata uses packed offsets plus bounded owner-local growth, with a
tested conservative fallback beyond the common capacities.

### 2026-08-14 — ARM64 native standalone final casts

Standalone `ref.cast` and `ref.cast_null` operations targeting final collector
struct or array types now validate through the checked native object view in
speed-oriented output. The compact reference remains the semantic result;
nullable null skips object resolution, while null in a non-null cast, i31
values, stale handles, wrong exact types, and malformed extents preserve the
cast-failure behavior. Open types, function types, compact objectives, and
disabled policy retain the helper.

Repeated casts from one local reuse one bounded raw-address certificate, so the
common region emits the complete checked resolver once without retaining it
across an unsafe opcode or safepoint. Tests cover final struct and array targets,
nullable null, non-null null and i31 failures, forced collector verification,
the open-type near miss, disabled policy, and compact fallback. On Apple M4 Max,
eight standalone struct casts improved from a 414.8 to 87.9 ns/op median
(-78.8%) through prepared invocation with zero B/op and allocations. Compilation
improved from 14.41 to 9.77 us/op (-32.2%), B/op fell from 27,288 to 17,912
(-34.4%), allocations fell from 49 to 44, and native output fell from 1,188 to
596 bytes (-49.8%).

### 2026-08-14 — shared nonzero reference provenance on ARM64

The shared one-byte value-provenance field now records a nonzero fact. ARM64
marks `ref.i31`, `ref.func`, and successful `ref.as_non_null` results. The fact
survives ordinary local storage, removes redundant `ref.as_non_null` and
`i31.get_s`/`i31.get_u` traps, and folds `ref.is_null` to false. Disabling
`value-facts` retains the original checks and comparisons. The new bit fits
existing storage padding, intersects conservatively at merges, and adds no side
table or per-operation allocation.

On Apple M4 Max, an eight-chain compile fixture that round-trips each `ref.i31`
through a local improved from a 10.851 to 9.911 us/op median (-8.7%), B/op fell
from 32,992 to 32,632, allocations fell from 32 to 28, and native output fell
from 564 to 356 bytes (-36.9%). Tests pin eight `ref.is_null` folds, sixteen
fact-driven null-check removals, and the disabled-policy fallback.

The same result contract now marks every non-null reference returned by an
ARM64 GC helper, so struct and array constructors retain the fact after their
safepoint and through locals. An eight-constructor compile fixture improved
from a 11.999 to 11.229 us/op median (-6.4%), B/op fell from 24,640 to 24,472,
allocations fell from 36 to 33, and native output fell from 888 to 712 bytes
(-19.8%). Nullable helper results remain deliberately unmarked.

Non-null reference parameters now seed the same fact in the existing
straight-line local fact table; mutable locals replace it on assignment and
control-bearing functions retain the established conservative fallback. An
eight-use parameter fixture improved from a 7.847 to 6.872 us/op compile median
(-12.4%), B/op fell from 15,096 to 14,928, allocations fell from 31 to 28, and
native output fell from 268 to 92 bytes (-65.7%). A nullable parameter is a
tested near miss.

ARM64 call-result placement now applies the same signature contract to register,
mixed-bank, wrapper, wide-wrapper, and synchronous host results. Direct consumers
and ordinary local sinks therefore retain non-null reference results without a
result-side table; nullable signatures remain unknown. With inlining disabled,
an eight-pair direct-call fixture improved from a 14.181 to 13.647 us/op compile
median (-3.8%), B/op fell from 35,920 to 35,752, allocations fell from 43 to 40,
and native output fell from 780 to 636 bytes (-18.5%). Tests cover direct
consumption, a local round trip, disabled facts, and the nullable-result near
miss.

Native final struct and array reference loads now preserve a field's declared
non-null result type when they publish the compact handle. Helper-backed loads
already receive the same contract from typed helper results. A combined
eight-pair struct/array fixture improved from a 38.050 to 36.971 us/op compile
median (-2.8%), B/op fell from 40,344 to 39,960, allocations fell from 44 to 43,
and native output fell from 7,032 to 6,840 bytes (-2.7%). Both native load
families, disabled facts, and a nullable field near miss are tested.

Typed `global.get` and `table.get` results now retain non-null reference
contracts as well. The lowering reads the existing module type directly and
adds no summary or retained state. An eight-pair imported-global/table fixture
improved from a 13.884 to 13.133 us/op compile median (-5.4%), B/op fell from
23,040 to 22,680, allocations fell from 34 to 30, and native output fell from
1,064 to 824 bytes (-22.6%). Nullable global and table types are tested near
misses.

ARM64 `select` now intersects the two candidate value facts and carries only
properties true on both sides into the selected register or fused local sink.
This is constant work at the existing eager sink and does not retain control
state. An eight-pair funcref fixture improved from a 14.259 to 13.678 us/op
compile median (-4.1%), B/op fell from 24,832 to 24,664, allocations fell from
37 to 34, and native output fell from 864 to 720 bytes (-16.7%). A nullable
candidate and disabled facts are tested fallbacks.

Materialized ARM64 `ref.is_null` and `ref.eq` results now carry the same boolean
and clean-upper-half facts as ordinary comparisons. Sixteen following
`i64.extend_i32_u` operations are removed in the focused fixture, reducing
native output from 416 to 352 bytes (-15.4%). Six-sample compile medians were
11.668 versus 11.672 us/op (+0.03%, treated as noise), with 26,640 B/op and 30
allocations/op unchanged. Disabling value facts retains every extension.

ARM64 `ref.test` and `ref.test_null` now publish that same semantic boolean
contract for constant function references, inline abstract-heap tests, dynamic
function-subtype tests, and collector-helper results. This is attached only
after successful lowering, so helper errors and disabled value facts preserve
the conservative path. A sixteen-test nullable-funcref fixture removes every
following `i64.extend_i32_u` and reduces native output from 452 to 388 bytes
(-14.2%). Six alternating fixed-work compile samples measured 9.009 versus
9.161 us/op (+1.7%, treated as noise), with 22,432 B/op and 30 allocations/op
unchanged. The disabled-policy fixture retains every extension.

Successful non-null ARM64 `ref.cast` operations now retain the value's guaranteed
nonzero state across native final-type checks, inline function and abstract-heap
checks, and collector-helper lowering. Nullable `ref.cast_null` deliberately
does not acquire the fact. An eight-cast helper-backed fixture removes eight
following `ref.as_non_null` checks and folds eight `ref.is_null` consumers,
reducing native output from 1,220 to 1,100 bytes (-9.8%). Six alternating
fixed-work compile samples improved from a 14.903 to 14.443 us/op median (-3.1%),
B/op fell from 43,720 to 43,552, and allocations fell from 55 to 52. Tests cover
the disabled-policy and nullable-cast near misses.

ARM64 `any.convert_extern` and `extern.convert_any` now preserve a proven
nonzero input across their bounded runtime identity bridge. Both runtime
conversion directions map null only to null, so this fact survives the helper
call even though raw reference identities and registers do not. Eight repeated
conversions remove eight `ref.as_non_null` checks and fold eight null tests,
reducing native output from 836 to 716 bytes (-14.4%). Six alternating
fixed-work compile samples improved from a 10.168 to 9.637 us/op median (-5.2%),
B/op fell from 24,904 to 24,736, and allocations fell from 49 to 46. Tests cover
both directions, disabled facts, and nullable-input near misses.

ARM64 `i31.get_s` and `i31.get_u` now record the clean upper half guaranteed by
their W-register shift instructions. This does not claim that the signed result
is already sign-extended to 64 bits; it only removes a subsequent unsigned i32
extension. A combined sixteen-get fixture removes all sixteen
`i64.extend_i32_u` instructions and reduces native output from 500 to 436 bytes
(-12.8%). Six alternating fixed-work compile medians were 10.077 versus
10.067 us/op (-0.1%, treated as noise), with 22,792 B/op and 34 allocations/op
unchanged. Disabling value facts retains every extension.

Native final ARM64 struct and array scalar reads now reuse the shared integer-load
fact function already used by ordinary memory loads. Packed signed reads retain
their exact sign-extension width, while every i32 read records only the clean
upper half guaranteed by its W-register destination. An eight-read packed-i8
struct fixture removes all eight following unsigned extensions and reduces
native output from 1,808 to 1,776 bytes (-1.8%). Six alternating fixed-work
compile medians were 12.754 versus 12.793 us/op (+0.3%, treated as noise), with
18,856 B/op and 45 allocations/op unchanged. Disabling value facts retains all
extensions.

AMD64 now records the same clean-upper-half facts for materialized
`ref.is_null`/`ref.eq` booleans and both i31 getter results. Its existing richer
GC-reference fact remains the sole owner of nullability; the shared one-byte
value field is used only for machine-width provenance. On a Ryzen 7 7800X3D,
the sixteen-boolean fixture shrank from 381 to 349 native bytes (-8.4%) and six
alternating fixed-work compile medians improved from 16.153 to 16.113 us/op
(-0.2%, treated as noise). The sixteen-i31-get fixture similarly shrank from
383 to 351 bytes (-8.4%) and compile medians improved from 16.204 to 16.137
us/op (-0.4%, treated as noise). B/op and allocations remained unchanged at
22,136/31 and 18,368/34 respectively; disabled facts retain every extension.

AMD64 `ref.test`/`ref.test_null` now publish the boolean result contract after
every successful constant, inline, dynamic-subtype, and helper-backed lowering.
Its eager integer `select` and compare-flags `select` paths also intersect the
two candidate value facts alongside their existing GC-reference intersection.
On the Ryzen 7 7800X3D, sixteen abstract function tests removed every following
unsigned extension and reduced native output from 353 to 321 bytes (-9.1%);
six alternating fixed-work compile medians were 16.000 versus 16.001 us/op.
Eight boolean selects reduced output from 409 to 393 bytes (-3.9%) and improved
compile medians from 15.432 to 15.281 us/op (-1.0%). B/op and allocations were
unchanged at 19,920/29 and 20,352/32. Disabled facts and a select with one
unknown candidate retain their extensions.

### 2026-08-14 — ARM64 native barrier-free reference writes

ARM64 now lowers proven-null and exact-i31 `struct.set` and `array.set`
operations for final collector-reference layouts through the checked native
object view. The shared collector contract proves that neither child requires a
generational write barrier. Null is recognized directly; i31 identity uses one
previously free bit in the existing one-byte value-fact field and survives the
established local and merge intersections. Unknown heap references,
function/external layouts, non-final or immutable layouts, disabled policy, and
Size/Embedded objectives remain helper-bound. Array writes retain
null-before-bounds trap order and both logical-index and physical-extent checks.

`ref.null` and `ref.i31` are now admitted inside the narrow raw-address reuse
envelope because both are pure and cannot move collector backing. Tests pin
eight resolver reuses, local-carried i31 provenance, disabled-fact and non-null
near misses, and 100 forced minor/major collection iterations with heap
verification. Full non-i31 heap-reference stores remain deferred until ARM64
has a validated slow-barrier path.

On Apple M4 Max, eight null struct writes improved from 723.5 to 271.9 ns/op
(-62.4%), and null array writes from 819.8 to 289.5 ns/op (-64.7%), with zero
execution B/op and allocations. Compile medians improved 31.4% and 27.8%; B/op
fell 36.8% and 39.9%, allocations fell from 38 to 34 and 40 to 39, and native
output fell from 1,376 to 668 bytes and 1,484 to 968 bytes.

Eight i31 struct writes improved from 666.0 to 211.5 ns/op (-68.2%), and i31
array writes from 784.6 to 238.7 ns/op (-69.6%), again with zero execution B/op
and allocations. Compile medians improved 23.9% and 18.8%; B/op was -0.1% and
+0.6%, allocations moved from 38 to 37 and stayed at 40, and native output fell
from 1,480 to 804 bytes and 1,588 to 1,104 bytes. Compact objectives retain the
existing helpers.

### 2026-08-14 — register-ABI call-move causality

The opt-in ledger now counts physical register-copy instructions emitted solely
to satisfy the internal register ABI. Argument counts cover GP/FP parallel-move
resolution, including bounded swap chains; result counts cover copies out of
ABI result registers and direct pinned-local sinks. Loads, constant
materialization, wrapper-slot traffic, and ordinary allocator copies remain
excluded, so this first call-traffic slice has an exact causal definition. The
two fixed counters live only in `CodegenStats`, render only when nonzero, and
code-neutrality tests retain identical native bytes with statistics disabled or
enabled.

The new counts identify a concrete next target. On the checked-in corpus,
Darwin/ARM64 records 11,248 argument and 78,303 result moves in Ruby, 2,476 and
2,739 in Lua, and 834 and 993 in wasm3. Native AMD64 records 112,865 argument
and 78,209 result moves in Ruby, 6,511 and 2,759 in Lua, and 1,679 and 988 in
wasm3. This makes Ruby's call-result preservation a shared priority and its
AMD64 argument shuffling a separate target-specific priority.

Six-sample fixed-work compile checks were neutral: on Apple M4 Max the general
FP-call median moved -1.7% and the leaf-FP median +0.6%, with 50 allocations/op
and B/op unchanged; on Ryzen 7 7800X3D, the mixed four-result register case
moved -0.8%, with 35 allocations/op and its ordinary B/op range unchanged.
Native AMD64 race and full backend tests pass.

### 2026-08-14 — bounded ARM64 direct-result residency

Direct internal ARM64 calls now retain GP results in X0/X1 while the caller
reloads its fixed local and global pin banks. Those banks exclude the ABI result
registers, so the results remain pinned symbolic locations without a protective
copy. Indirect calls, wrapper calls, and the fused pinned-local sink retain their
existing conservative paths. The rule is an immutable per-compilation option
with an environment kill switch, and its opt-in statistics report each removed
copy as `call-result-resident`.

On `ruby.wasm`, the bounded rule removes 75,314 of 78,303 attributed ARM64
result moves. Total native code falls from 41,048,240 to 40,745,696 bytes
(-302,544 bytes, -0.74%). The 16-call mixed four-result fixture falls from 420
to 292 native bytes with unchanged zero-allocation execution; six-sample medians
were effectively flat at 24.96 versus 24.79 ns/op. Ruby compile medians moved
only +0.25%, with ordinary allocations and B/op unchanged; the focused compile
fixture moved +0.6%, also with 33 allocations/op unchanged.

The analogous AMD64 rule was measured and rejected rather than shipped. It
reduced the focused fixture from 363 to 267 native bytes, but repeated isolated
Ryzen 7 7800X3D runs slowed from a 29.94 to 30.94 ns/op median (+3.3%). Modern
AMD64 move elimination makes these copies much cheaper than their byte count
suggests, so AMD64 retains the existing protective copies pending a different
selection or alignment strategy.

A narrower result-to-next-call-argument form was also rejected. It matched only
134 of Ruby's 112,865 argument moves, and a 32-chain focused workload slowed
from a 67.02 to 73.42 ns/op median (+9.5%) after removing both copies. Future
AMD64 argument work should therefore target allocation and parallel-shuffle
shape without shortening every call/return interval indiscriminately.

### 2026-08-14 — AMD64 argument-move causality refinement

The opt-in call ledger now partitions AMD64 register-argument instructions by
integer, mixed-bank, and tail-call lowering, and separately marks the bounded
parallel resolver's swap cases. The three fixed counters add 24 bytes only to
an opt-in per-function `CodegenStats`; ordinary compilation retains nil stats,
the same emitted bytes, and no new allocation. Eight-sample native compile
medians were neutral at 8.51 versus 8.47 us/op, with 35 allocations/op and B/op
unchanged.

On native AMD64 Ruby, 112,404 of 112,865 argument moves (99.6%) come from the
ordinary integer-call path, 461 from mixed calls, and none from tail calls. Only
five integer moves are cycle-breaking swaps; mixed calls add four FP swaps.
A temporary stats-only source probe further attributed 103,176 integer
mismatches to pinned locals, 5,775 to deferred expressions, 2,488 to memory
references, and 970 to ordinary values. The probe was removed after measurement
to keep disabled statistics off the production compile path.

The next AMD64 step is therefore not resolver tuning. It is bounded
call-position-aware local ownership: arrange a dying or call-adjacent pinned
local in its ABI argument register before the call, without globally pinning
fixed-role RAX/RCX/RDX/R8 and without changing call/return spacing.

#### Follow-up demand audit: safe whole-function pin placement is too narrow

A temporary opt-in source probe partitioned every mismatched pinned-local
integer argument by ABI position across the native AMD64 corpus. It was removed
after measurement and made no code-selection decisions:

```text
arg0 / RAX: 72000
arg1 / RCX: 35201
arg2 / RDX: 17121
arg3 / R8:   5146
arg4 / R9:   1478
arg5 / R10:   341
arg6 / R11:    82
total:      131369
```

The first four fixed-role registers account for 129,468 mismatches (98.6%).
Only 1,901 (1.4%) target the safe extended pin pool R9-R11. Merely preferring a
whole-function local pin for those three registers therefore cannot address the
measured problem and was not implemented. A future retry needs bounded
call-site ownership transfer for a dying local, with explicit fixed-register
pressure accounting; it must not globally pin RAX/RCX/RDX/R8.

### 2026-08-15 — ARM64 argument-move causality refinement

The same opt-in call ledger now partitions ARM64 ordinary register-ABI argument
instructions between integer-only and mixed GP/FP lowering. Each exact emitted
move, including moves used to break or combine a parallel-copy cycle, increments
the headline total and one cause. Register-ABI tail calls already canonicalize
arguments to frame slots and reload their target banks, so they emit no
register-to-register argument copies and retain a zero tail-move count.

This changes only nil-safe statistics calls and the fixed opt-in counters;
generated code and ordinary compiler storage are unchanged. Tests cover both
integer and mixed-bank attribution. Across the 64-module Balanced ARM64 corpus,
the split records 67,755 integer-call moves and 417 mixed-call moves: 99.4% of
the measured traffic is in the integer path. The largest integer contributors
are Script (21,510), SQLite (11,942), Ruby (10,882), Esbuild (10,537), and
Regexmatch (5,492). Ruby has the largest mixed count at 192.

The corpus also contains only one hit for the existing generated two-swap
machine rule, again in Ruby. A longer swap-chain rule is therefore not being
added. As on AMD64, the next material ARM64 argument step must influence
call-adjacent value ownership before parallel resolution rather than expand the
resolver for rare cycles.

### 2026-08-14 — bounded AMD64 forward-merge next use

Forward block and `if` merges now keep a memory-only pinned local lazy when a
copied Wasm reader proves that the local is overwritten, returned past, or dead
at the physical function end before its next read. The scan uses two register
masks, has a hard 64-operation fuel cap, and falls back on malformed input,
nested control, calls, tail transfer, EH, and exhaustion. Loop headers retain
their fixed eager contract. No CFG, heap allocation, or retained liveness
interval is introduced.

This removes 2,275 of Ruby's 213,965 AMD64 control-merge reloads and 11,920
native bytes. A 32-merge native Ryzen 7 7800X3D workload improves from a 39.67
to 38.89 ns/op median (-2.0%), shrinks from 1,558 to 1,270 native bytes, and
remains at zero execution allocations. Six fixed-work Ruby compile samples move
from an 890.86 to 896.41 ms median (+0.62%); B/op and allocations are unchanged.
Positive, next-read near-miss, disabled-policy, and fuel-exhaustion cases retain
deterministic conservative behavior.

### 2026-08-14 — finite AMD64 LeafScalar ABI class

AMD64 direct calls can now use one finite pin-preserving internal ABI class for
tiny integer-only leaves. Admission requires a register-ABI signature with at
most four parameters, no declared locals, calls, control flow, or global access,
and at most 12 estimated stack-arena nodes. A memory-touching leaf is admitted
only when the existing transitive effect summary proves that it cannot execute
`memory.grow`. The callee reserves the complete GP and FP local-pin banks; the
caller can therefore keep dirty pinned locals and value-pinned globals in their
registers across the call. All other calls retain the General contract.

The class table is one byte per local function and is allocated only after the
module admits at least one non-inlined class member. Targets whose ordinary calls
will all be inlined are excluded: reserving registers in a retained export or
standalone body cannot help those callers. With default inlining, Ruby therefore
emits byte-for-byte identical native code to the disabled scalar policy and the
scalar classification alone allocates no class table. With inlining deliberately disabled, Ruby
admits 876 leaves across 6,461 direct calls, removes 1,259 call-preservation
stores and 3,978 reloads, and shrinks by 47,885 native bytes.

On a serialized Ryzen 7 7800X3D workload containing 128 calls per invocation,
five two-second samples improve from a 172.1 to 155.7 ns/op median (-9.5%) and
shrink from 2,497 to 1,979 native bytes (-20.7%), with zero B/op and allocations.
Five fixed-work default-inline Ruby codegen samples remain neutral at an 891.20
ms median versus 893.12 ms with the policy disabled (-0.21%, treated as noise);
B/op and allocations remain within run-to-run noise. Tests
cover execution with the class enabled and disabled, `memory.grow`, control and
pressure-cap near misses, full-inline exclusion, deterministic recompilation,
and isolated interaction with the call- and merge-next-use optimizers.

### 2026-08-14 — finite AMD64 LeafFP ABI class

The same module-precomputed contract now admits tiny FP and mixed scalar leaves
independently under `abi-leaf-fp`. Admission retains the scalar class's no-local,
no-call, no-control, no-global, effect-safe-memory, and 12-node pressure bounds,
and adds a hard maximum of four arguments in each register bank. This cap is
required on AMD64 because XMM4-XMM7 are both later FP argument registers and
members of the caller local-pin bank. Admitted callees reserve every GP and FP
pin; direct mixed callers skip local/global preservation. Exact GC caller-root
maps force the General call path so their canonical frame-slot contract remains
unchanged.

Ruby admits three FP leaves across 60 direct calls, removes 11 preservation
stores and 29 reloads, and shrinks by 1,042 native bytes. Lua and wasm3 are exact
near misses with unchanged code. On a serialized Ryzen 7 7800X3D workload with
128 four-argument FP calls per invocation, seven one-second samples improve from
a 165.2 to 156.2 ns/op median (-5.4%) and shrink from 6,879 to 3,396 native bytes
(-50.6%), with zero B/op and allocations. Five fixed-work Ruby compile samples
remain neutral at 915.78 ms with the option disabled versus 917.29 ms enabled
(+0.17%); B/op rises by about 19 KiB (+0.05%) for the one-byte-per-function
class table and allocations remain within run-to-run noise.

### 2026-08-14 — rejected dynamic-import context guard

An AMD64 prototype used the already-instantiated equality of target and caller
context pointers to skip redundant context copies for direct host imports. The
first form guarded both the pre-call target copy and post-call caller restore.
Across 65 imports per invocation, host calls improved from 18.116 to 17.703 us
(-2.3%), but real cross-instance calls regressed from 539.4 to 542.5 ns (+0.6%)
because every foreign target paid two failed guards. A narrower pre-call-only
form retained a host improvement from 17.683 to 17.350 us (-1.9%) but worsened
the cross-instance regression to 538.4 versus 542.7 ns (+0.8%). All measurements
used seven serialized one-second samples on the Ryzen 7 7800X3D; cross-instance
execution remained zero-allocation.

The prototype and its policy bit were removed. The viable seam is an
instantiation-selected dispatch target: host imports should call a path that
never copies instance context, while cross-instance imports should target a
dedicated context-transfer trampoline. That design avoids a per-call kind guard
and belongs with the finite dispatch-cell classification, not the shared Wasm
callsite lowering.

### 2026-08-14 — AMD64 BMI2 variable shifts

The fixed-register campaign first tested a bounded relaxation of the existing
expanded unsigned multiply-high rule. A temporary structural counter found one
match across all 64 checked-in modules: the dedicated `xjb-mulhi.wasm` fixture,
where the current tail-only rule already fires. The relaxation and its
four-local liveness proof were removed; there is no corpus demand for carrying
that machinery yet.

Variable shifts are different. The same corpus contains 5,463 variable shift or
rotate sites, and 4,999 are `shl`, signed `shr`, or unsigned `shr` operations
eligible for BMI2. A new immutable `bmi2-shifts` policy selects SHLX, SARX, and
SHRX after the existing host CPUID gate, avoiding the legacy RCX/CL move and
spill path. Variable rotates retain the conservative legacy lowering; non-BMI2
hosts reject explicit enablement, and compiled artifacts reuse the existing
BMI2 requirement bit.

On the Ryzen 7 7800X3D, six serialized samples of 128 dependent variable shifts
improve from a 28.96 to 28.66 ns/op median (-1.0%), with zero execution B/op and
allocations. Focused compilation improves from 30.97 to 27.55 us/op (-11.0%);
the median remains 95,904 B/op and 28 allocations. Real JSON serialization
improves from 22.74 to 22.46 us/op (-1.2%), deserialization from 40.07 to 39.77
us/op (-0.8%), and SIMD serialization from 26.76 to 26.51 us/op (-0.9%). The
64-module native total falls by 13,141 bytes, including 5,024 bytes in Ruby,
1,248 in esbuild, 1,214 in regex, and 960 in SQLite. Encoder goldens, full-width
execution cases, the complete AMD64 backend, the executable corpus, and native
race tests pass.

### 2026-08-15 — AMD64 unsigned three-way compare fusion

The bounded Valent selector now recognizes the exact unsigned three-way forms
`(a > b) - (a < b)` and its reversed result when both comparisons repeat the
same simple locals, globals, or constants. AMD64 emits one `CMP`, `SETA`, and
`SBB result, 0`; `SETcc` preserves the comparison flags, so the final operation
maps less/equal/greater to -1/0/1 without materializing and subtracting two
booleans. Mutable loads are excluded because folding them would change their
observable read count. A dedicated immutable `three-way-unsigned` policy keeps
the ordinary lowering as an exact A/B and fallback path.

An opt-in corpus probe found 54 exact sites: 21 in `bignum.wasm`, 9 in
`regexmatch.wasm`, and 24 in `script.wasm`. Their final native images shrink by
256, 48, and 256 bytes respectively. On the Ryzen 7 7800X3D, six serialized
samples of a 128-site fixture improve from a 63.33 to 43.64 ns/op median
(-31.1%); native function bytes fall from 4,193 to 3,101 (-26.0%), with zero
execution B/op and allocations. Focused compilation improves from an 87.50 to
76.53 us/op median (-12.5%) while retaining 31 allocations/op and approximately
211 kB/op. Full-width i32/i64 execution, both result orientations, policy and
near-miss cases, the AMD64 race suite, and the full repository suite pass
natively.

### 2026-08-15 — AMD64 widened `local.tee` carry fusion

The wide-arithmetic campaign now recognizes the exact producer/consumer form
`iN.add; local.tee $sum; local.get $operand; iN.lt_u;
i64.extend_i32_u`, where `$operand` is one of the addition's original local
operands. AMD64 forces the final operation to be an ordinary full-width `ADD`,
homes `$sum` with flag-neutral moves, and materializes the widened carry directly
from CF. Flag-neutral LEA/tree covers and `INC` are excluded from this path. A
dedicated immutable `tee-add-carry` policy retains the ordinary compare path as
the exact fallback.

The widening is an intentional cost gate. An earlier prototype also accepted an
unextended comparison and found 287 apparent corpus sites, but most were
allocator overflow checks already handled more cheaply by compare-to-branch
fusion. That broad form increased several native images and was removed. The
retained form has 65 exact sites: 34 in Ruby, 10 in SQLite, 9 in Lua, and one
each in the script and esbuild artifacts after excluding nested additions that
already have an associative-tree cover. Their combined native output falls by
336 bytes; the isolated 128-site function falls from 3,141 to 2,264 bytes.

On the Ryzen 7 7800X3D, six serialized one-second samples of that fixture improve
from a 46.06 to 36.35 ns/op median (-21.1%), with zero execution B/op and
allocations. Focused compilation improves from 101.33 to 66.29 us/op (-34.6%);
B/op falls from about 211 kB to 96 kB and allocations from 33 to 31 because the
exact reader window consumes the redundant compare and extension without
building their Valent nodes. Six fixed-work SQLite compile samples remain within
the corpus gate at 79.91 ms disabled versus 80.25 ms enabled (+0.4%), with B/op
and allocations unchanged within run-to-run noise. Full-width i32/i64 execution,
policy and single-precondition near misses, every affected corpus module, the
complete AMD64 backend, the repository suite, and native race tests pass.

### 2026-08-15 — AMD64 widened carry arithmetic

The bounded Valent selector now also recognizes exact nontrapping
`x + i64.extend_i32_u(a <u b)` and
`x - i64.extend_i32_u(a <u b)` trees, plus the corresponding unsigned-greater
forms when both comparands are already simple nontrapping values. It materializes
`x` before the comparison, emits `CMP` last, and consumes CF directly with
`ADC x,0` or `SBB x,0`; greater-than reverses only the final CMP operands so CF
represents the same predicate. Carry on the left is accepted only for
commutative addition. Signed comparisons, non-simple greater-than comparands,
carry-left subtraction, trapping/deferred memory work, and every other shape
retain ordinary lowering. The immutable `widened-carry-arith` policy is the A/B
and rollback boundary.

The checked-in corpus contains 200 exact sites after backend lowering: 140 in
Ruby, 33 in the script artifact, 19 in Lua, and 8 in SQLite. Their combined native
output falls by 1,322 bytes. On the Ryzen 7 7800X3D, six serialized one-second
samples of a mixed LT/GT 128-site fixture improve from a 36.51 to 30.38 ns/op
median (-16.8%); native function bytes fall from 2,669 to 2,157 (-19.2%), with
zero execution B/op and allocations. Focused compilation improves from an 83.56
to 67.73 us/op median (-18.9%), with about 211 kB/op and 33 allocations
unchanged. Six fixed-work Ruby compile samples remain neutral at 905.42 ms
disabled versus 905.84 ms enabled (+0.05%), with B/op and allocations within
run-to-run noise.
Full-width add/sub execution, operand orientation, policy and near-miss tests,
the affected corpus modules, the complete AMD64 backend, the repository suite,
and native race tests pass.

### 2026-08-15 — deferred general tiny if-conversion

A streaming scan of all 64 benchmark modules found only 16 load-free integer
`if (result ...)` regions whose two arms are bounded constants, local reads, or
one local-plus-immediate operation. All 16 are synthetic `isa_ctl.wasm` cases
already served by ARM64's narrower local-sink fast path; the production corpus
has no candidate. Across all 1,333 checked-in Wasm artifacts, the additional
matches are repeated regression fixtures and command-wrapper boilerplate, not a
measured hot workload.

The real small-result regions observed in Ruby put a memory load in one arm.
Speculating that load for `CMOV`/`CSEL` would change Wasm trap behavior, so they
remain outside the plan's initial pure, nontrapping admission class. A general
if-converter would therefore add copied-reader parsing, dual-arm register
pressure, and target cost policy without a production consumer. No compiler
state was added; this item remains deferred until an opportunity counter finds
a representative load-free workload or a separate trap-preserving predication
scheme is justified.

### 2026-08-15 — rejected AMD64 structured fast-return shrink wrapping

The first exact shrink-wrap prototype targeted the one strong pure corpus shape:
`fib_rec.wasm` compares its i32 parameter, returns that parameter as i64 on the
fast arm, and performs recursive calls only on the slow arm. A bounded prefix
reader emitted the comparison and fast return before the normal register-ABI
frame, while the false edge entered the unchanged prologue and body. The fast
edge retained its entry interrupt poll; loops, EH, locals beyond the parameter,
other signatures, other comparisons, and trapping fast arms were excluded.

This did not pass the native throughput gate. On the Ryzen 7 7800X3D, six
serialized one-second samples of fib(20) moved from a 26.17 to 26.41 us/op
median (+0.9%), with zero B/op and allocations. The repository's standard
fib-rec benchmark at fib(25) was neutral within 0.1% across six two-second
samples. The slow recursive arm pays for the duplicated comparison and branch,
offsetting the frame work removed at leaves. A Rosetta run had misleadingly
shown an approximately 20% improvement, reinforcing that target-native evidence
is required for machine-local transforms. The reader, emitter, policy bit,
tests, and benchmarks were removed; broader shrink wrapping remains deferred
until a shape can enter its slow body directly without duplicating hot work.

### 2026-08-15 — deferred inverted widened carries

An AMD64 extension admitted widened unsigned `<=`/`>=` booleans into the
existing `ADC`/`SBB` carry rule by complementing CF with one `CMC`. Full-width
add/sub execution and the generated encoding passed, and a mixed 128-site
fixture improved materially without execution allocation. However, a distinct
opt-in counter found zero inverted-carry sites across all 64 benchmark modules;
all 200 real widened-carry hits remain `<`/`>`. The rule, counter, encoder method,
and tests were removed. It should return only with production-corpus demand,
not because the isolated sequence is locally cheaper.

### 2026-08-14 — rejected ARM64 bulk-memory register pairs

An ARM64 prototype replaced the 32- and 64-byte copy/fill loop bodies with
pre/post-indexed Q-register LDP/STP pairs. The encodings and forward, backward,
overlap, and tail behavior passed native execution tests, and the dynamic copy
function shrank from 816 to 736 native bytes; fill shrank from 428 to 408 bytes.

The smaller loops were not faster on Apple M4 Max. Six serialized 4 KiB samples
moved forward copy from a 40.02 to 41.96 ns/op median (+4.8%). At 256 bytes,
copy moved from 9.76 to 10.39 ns/op (+6.4%). Fill improved from 35.51 to 35.25
ns/op at 4 KiB (-0.7%) but regressed from 7.38 to 7.49 ns/op at 256 bytes
(+1.5%). All measurements retained zero B/op and allocations. The encoder,
policy bit, generated schema entry, loop changes, and benchmarks were removed;
the existing separate vector loads/stores plus pointer increments remain the
measured throughput choice.

### 2026-08-14 — deferred poll coalescing and stack-fence hoisting

The current module scan retains bounded direct-call edges only long enough to
propagate semantic effects. It does not retain the complete entry-reachability
proof needed to remove a poll from shared function code: exports, the start
function, element/table references, `ref.func`, tail entries, host reentry, and
every direct caller would all have to be excluded or bounded. Moving the same
poll to each direct call site would save no execution work, while inheriting an
older caller poll has no documented instruction or time cancellation budget.
Poll coalescing is therefore deferred rather than admitted on a leaf-only
approximation.

Stack-fence hoisting has the same reachability requirement plus a fixed-point
over finalized frame sizes and cumulative direct-call depth. The existing
backends already omit the fence for call-free functions whose conservative
frame bound is at most 4 KiB, so a new leaf class has no remaining work to
remove. Non-leaf chains remain fenced until an exact acyclic, externally rooted
chain proof can be built without retaining a general CFG. No production code or
compiler storage was added for either experiment.

### 2026-08-14 — deferred AMD64 FP regional bank

A temporary opt-in counter broadened the existing AMD64 interval-region local
filter from integers to scalar `f32`/`f64` while retaining every current
admission rule: 128--16,384 body bytes, 16--256 locals, no calls, no control
flow, no bulk memory, and a minimum local score of two. It found zero qualifying
FP locals across all 64 checked-in benchmark modules. Useful corpus FP work is
loop-shaped, so adding a second owner bank and FP eviction path before bounded
loop/control regions would add allocator state without a production consumer.
The probe was removed; FP/vector regional residency remains ordered after a
measured structured-region extension.

A 2026-08-15 re-audit after forward-merge residency reached the same ordering
decision with the current eleven-register FP/vector pin bank as its baseline.
There are still zero score-qualified overflow locals in functions admitted by
the straight-line interval cache. The remaining 98 overflow candidates all
cross a barrier that needs explicit ownership convergence:

```text
call-free structured control: 62
call-making functions:         36
```

The structured candidates are confined to two `blake-as-simd` functions (50)
and `nbody.step` (12). The call-making candidates are concentrated in
`raytrace` (28), with three in esbuild, three in Ruby, and two in SQLite. The
temporary stats-only probe was removed. A second dynamic XMM owner bank should
therefore be built only with the call/control-separated region contract; adding
it to the current straight-line admission still has no production consumer.

### 2026-08-14 — ARM64 barrier-free reference array fills

ARM64 now selects the existing `ArrayFillNoBarrier` runtime helper for reference
`array.fill` operations whose value is proven null or exact i31. It reuses the
same immutable `gc-native-final-ref-set` policy and value facts as native
single-element stores; unknown heap children, disabled facts for i31, and a
disabled policy retain the full barrier helper. The call ABI, helper transition,
collector implementation, native code size, and compiler storage are unchanged.

Tests cover null and i31 selection plus all three conservative near misses. Six
serialized Apple M4 Max collector samples isolate the work removed behind the
unchanged helper boundary: the median for 16 elements moved from 29.58 to 28.56
ns/op (-3.4%), and 256 elements from 47.27 to 44.98 ns/op (-4.8%). At 4,096
elements the medians were 206.0 and 204.5 ns/op (-0.7%, treated as noise) as the
fill itself dominates. Every case remains at zero B/op and allocations. The
ARM64 backend, shared-fact, and collector suites pass.

### 2026-08-15 — deferred ARM64 native constructors

The AMD64 native constructor helper IDs are not portable helper
specializations. Their Go dispatch first prepares an AMD64-only allocation
reservation containing validated handle, chunk, epoch, type, and object-space
state; substantial backend stubs then consume that reservation, initialize the
object, and publish it in the required order. The non-AMD64 preparation hook is
intentionally a no-op. Selecting those IDs from ARM64 without the matching
runtime contract and target stubs would therefore add transition work without
creating a native allocation path.

A smaller compiler-scratch experiment was also rejected. Reusing the dynamic
`[]wasm.ValType` signatures built for `struct.new` and `array.new_fixed` left a
32-constructor Apple M4 Max compile fixture unchanged at 25 allocations/op in
six samples per form: escape analysis already keeps those temporary slices off
the heap. The buffer would only retain additional worker memory, so the
prototype was removed.

ARM64 native constructors remain deferred until one bounded constructor class
can own the complete platform contract: reservation preparation, exact layout
and type validation, handle/object bounds, reference validation, publication
order, collection invalidation, fallback, and target-native execution tests.
No production compiler or runtime state was added by this audit.

### 2026-08-15 — rejected ARM64 bitmask-to-boolean zero tests

The corpus contains six adjacent SIMD `bitmask; i32.eqz` sites in
`json-as-simd.wasm`: five `i16x8` and one `i8x16`. A bounded copied-reader
prototype reduced lane sign bits directly and materialized the final zero-test
boolean. On an Apple M4 Max, a 64-site i16 value-producing fixture improved
from a 33.20 to 14.90 ns/op median (-55.1%), and its native bytes fell from
4,808 to 1,988. Focused compilation also improved without changing B/op or
allocations.

The real consumer shape is a branch, not an integer value. Materializing 0/1 at
the SIMD rule boundary prevents the existing compare-to-branch path from
carrying the condition through the following `if`. In six alternating two-second
JSON deserialization pairs, the prototype was slower in every pair (by 0.6 to
25.3 ns/op; the largest gaps are treated as system noise), and the independent
six-sample batch median regressed about 0.5%. The implementation, tests,
benchmark, and policy extension were removed. A future retry must fuse through
`if`/`br_if` as a condition token rather than stopping at a materialized
boolean.

### 2026-08-15 — ARM64 bitmask zero-branch fusion

The accepted follow-up carries the six exact `bitmask; i32.eqz; if/br_if`
shapes through the branch rather than materializing a boolean. The bitmask
lowering adds one bounded deferred node only when a copied reader proves the
two-operation consumer. Ordinary value consumers, `i64x2`, disabled policy,
malformed suffixes, and every non-branch near miss retain eager scalar-mask
lowering. At the existing compare-to-branch boundary, Railshot flushes lower
operands first, reduces the i8/i16/i32 lane sign bits, emits the final `CMP`, and
lets the established condition path consume NZCV directly. No condition object,
retained CFG, heap allocation, or finalizer responsibility was added.

On Apple M4 Max, six serialized samples of a 64-branch i16 fixture improve from
a 31.24 to 13.87 ns/op median (-55.6%), with zero execution B/op and
allocations. Native function bytes fall from 4,516 to 1,956. Focused compile
median improves from 48.71 to 40.88 us/op (-16.1%); compile traffic remains
104,656 B/op and 34 allocations. Six alternating real `json-as-simd` pairs move
deserialization from a 248.4 to 245.6 ns/op median (-1.1%), leave serialization
within noise, and shrink the module by 224 native bytes. Exact i8/i16/i32 `if`
and `br_if` execution, policy and near-miss cases, the ARM64 race suite, and the
full repository suite pass.

---

# 1. North-star architecture

The target architecture is:

```text
Wasm module
    │
    ├── compact module/function summary scan
    │     local use and last-use summaries
    │     call graph and effect summaries
    │     loop/region candidates
    │     branch hints
    │     definite-assignment information
    │     optimization admission and scratch sizing
    │
    └── one forward Wasm lowering traversal
          │
          ├── Valent expression trees
          ├── packed value/provenance facts
          ├── rematerialization recipes
          ├── regional register residency
          ├── effect-aware calls
          ├── generated target-rule selection
          └── bounded symbolic machine window
                    │
                    ▼
          completed bounded finalizer
          branch/layout/relocation/compaction
                    │
                    ▼
               executable code
```

“Single-pass” means:

> One semantic traversal of the Wasm body, with bounded pending state and a relocation-like finalizer.

It does **not** mean every opcode must irreversibly emit its final instruction bytes immediately.

## Non-negotiable constraints

1. **No production whole-function SSA or general machine IR.**
2. **No retained CFG for ordinary compilation.**
3. **No whole-function live intervals or graph-coloring allocator.**
4. **No global PRE, general LICM, trace scheduling, or online e-graph.**
5. **No per-operation heap allocation.**
6. **Every bounded optimizer has a conservative fallback.**
7. **Serial compilation remains the predictable-memory mode.**
8. **Steady-state execution remains zero-allocation where it is currently zero-allocation.**
9. **Native-code growth remains budgeted because larger code also consumes memory and I-cache.**
10. **Semantic facts are shared; target instruction choices remain architecture-specific.**

The intended compiler workspace remains:

```text
module summaries
+ worker count × bounded worker scratch
+ O(locals + control depth + configured region capacity)
```

---

# 2. Current starting point

Railshot is already beyond an ordinary baseline compiler. The recently merged performance campaign added bounded ARM64 SIMD and conversion peepholes, removed redundant AMD64 memory32 work, reused substantial compiler scratch, and preserved zero execution allocations. Its reported AMD64 comparison showed Wago ahead of wazero on both compile and execution geometric means in that test matrix.

There are already two especially important precedents for this plan:

* AMD64 regional residency caches up to nine integer locals, but only for call-free, control-free functions within tightly bounded body/local limits.
* Direct-call liveness already uses a copied Wasm reader with a hard 64-operation fuel limit and two register masks to avoid unnecessary pre-call local stores.

Those prove the core strategy:

```text
small bounded lookahead
+ compact facts
+ safe fallback
= meaningful execution gains without a compiler architecture reversal
```

Several prerequisites remain open on current main:

* Optimizer settings still need to become immutable per-compilation state rather than architecture-global mutable bindings.
* Compiler arenas, dense tables, generation counters, combined scans, and effect summaries remain an explicit open workstream.
* Generalized upper-bit/value provenance remains open.
* ARM64 still lacks several proven AMD64 WasmGC native fast paths.

These should form the beginning of the critical path.

---

# 3. The coherent dependency graph

```text
P0: Measurement + immutable policy + scratch ownership
        │
        ├──────────────┐
        ▼              ▼
P1: Semantic facts   Offline rule/verifier foundation
    + rematerialize      │
        │                │
        ├──────────┬─────┘
        ▼          ▼
P2: Regional     P3: Call effects
    residency        + internal ABI classes
        │          │
        └─────┬────┘
              ▼
P4: Generated Valent selection
    + producer-linked machine combiner
              │
        ┌─────┴──────────┐
        ▼                ▼
P5: Target campaigns   P6: Structured transforms
    AMD64/ARM64/SIMD       if-convert/shrink-wrap
    WasmGC/bulk memory     frame/fence/poll placement
        │                │
        └───────┬────────┘
                ▼
P7: Profile-guided and research experiments
```

The order matters:

* Regional allocation without reliable facts causes spills and miscompilations.
* Call optimization without effect summaries forces conservative invalidation.
* A machine combiner without provenance cannot safely eliminate extensions or masks.
* Target pattern expansion without a rule/verifier framework becomes unmaintainable.
* Structured motion without stable effects and trap facts is too risky.

---

# 4. Phase 0 — establish the control plane

## 4.1 Rebaseline the remaining gaps

The existing broad performance issue contains numbers from before several major runtime and compiler improvements. Replace it with a current matrix that separates:

| Dimension    | Required splits                                          |
| ------------ | -------------------------------------------------------- |
| Architecture | AMD64, Darwin ARM64, Linux ARM64                         |
| Bounds       | Explicit, signal/guard                                   |
| API boundary | Ordinary invoke, prepared invoke, direct prepared        |
| Calls        | Direct, indirect, `call_ref`, host, cross-instance, tail |
| Workload     | Scalar, FP, SIMD, branch, local-heavy, memory, GC        |
| Lifetime     | Fresh instance, warmed instance, sustained instance      |
| Compilation  | Decode, validate, backend, full compile                  |
| Memory       | B/op, allocs/op, live heap, peak RSS, worker scratch     |

The goal is not another aggregate leaderboard. It is to identify the dominant remaining cost for each losing row:

```text
frame traffic
register moves
host boundary
call preservation
branching
bounds/address setup
SIMD synthesis
GC transition
collector work
I-cache
```

## 4.2 Add a performance-causality ledger

Extend opt-in statistics with per-function counts for:

```text
parameter-home stores
declared-local zero stores
ordinary spill stores/reloads
control-merge stores/reloads
call-preservation stores/reloads
call argument moves
call result moves
local-region cache hits/misses/evictions
rematerializations
memory base/size derivations
global/table directory derivations
bounds instructions
conditional and unconditional branches
host transitions
GC helper transitions
native GC stub calls
GP↔FP/vector transfers
SIMD shuffle/extract/insert operations
```

Every stack access should have a cause. “967 stack accesses” is actionable only when the report says:

```text
420 local-frame reloads
180 call-preservation reloads
140 merge canonicalizations
125 parameter homes
102 ordinary spills
```

Stats remain absent from normal compilation unless requested.

## 4.3 Remove process-global optimizer state

Complete issue #399 before introducing many more policies and experimental features.

Materialize one immutable backend policy:

```go
type BackendPolicy struct {
    Objective OptimizationObjective
    Effort    CompileEffort
    Features  TargetFeatures
    OptBits   [2]uint64

    RegionBudget      uint16
    MachineWindowSize uint8
    RewriteFuel       uint16
    MaxWorkers        uint8
}
```

The completed code-size objectives remain the product-level policy. Add only one orthogonal compiler-effort dimension:

```text
Minimal
Balanced
Throughput
```

Do not expose every region/window/fuel constant publicly.

This removes same-architecture compile serialization, makes concurrent A/B testing safe, and gives later optimizations an immutable policy source. Current architecture-global bindings serialize whole compilations and make independent settings fragile.

## 4.4 Complete the required subset of reusable scratch

Issue #316 should be divided rather than implemented as one enormous PR.

The first subset should provide reusable storage for:

```text
local facts
local next-use summaries
region descriptors
call-effect summaries
machine-window operations
rule matcher scratch
relocations and metadata marks
```

Use:

* Dense slices indexed by local/function/memory/table index.
* Generation counters where clearing dominates.
* Fixed inline storage for common tiny functions.
* Owner-local reuse on the serial path.
* Worker-owned scratch under parallel compilation.
* Capacity trimming after pathological functions.

Do not use one global `sync.Pool`.

## Phase 0 exit criteria

```text
same config + same module → deterministic bytes under concurrent compilation
same-architecture independent compiles no longer serialize
all performance counters have zero cost when disabled
one combined summary scan owns new summaries
pathological cap tests show bounded scratch
```

---

# 5. Phase 1 — build the semantic fact layer

This is the foundation for almost every later optimization.

## 5.1 Packed value provenance

Implement issue #438 as one architecture-neutral fact system.

A practical first representation:

```go
type ValueFacts uint16

const (
    FactUpper32Zero ValueFacts = 1 << iota
    FactSignExt8
    FactSignExt16
    FactSignExt32
    FactBoolean
    FactNonZero
    FactKnownAligned2
    FactKnownAligned4
    FactKnownAligned8
    FactImmutable
    FactNonNullRef
    FactExactRefType
    FactFreshRef
)
```

The exact representation can use packed fields rather than independent flags, but it should remain inline in existing value/storage state.

### Producers

Facts can originate from:

* Constants.
* Integer comparisons.
* `i32` arithmetic with known native-width behavior.
* Signed and unsigned loads.
* Explicit extensions/truncations.
* Known local frame loads.
* Known host/import result contracts.
* Immutable globals.
* GC constructors and casts.

### Joins

At a structured merge:

```text
result facts = intersection of reachable predecessor facts
```

For exact types:

```text
same exact type on every predecessor → retain exact type
otherwise → retain only common super-facts
```

### Invalidation

Drop or weaken facts across:

* Unknown host calls.
* Cross-instance calls without a contract.
* GC safepoints for raw resolved addresses.
* Physical spills that lose width information.
* Partial-register writes.
* Opaque plugin operations.
* Mutable loads and stores where relevant.

GCC’s combine and redundant-extension work rely on explicit known-bit/sign information rather than assuming native cleanliness from the source-language type.

## 5.2 First-class rematerialization

Introduce bounded recipes for values cheaper to recompute than spill and reload:

```go
type RematKind uint8

const (
    RematNone RematKind = iota
    RematConstant
    RematZeroExtend
    RematSignExtend
    RematBaseDisp
    RematScaledAddress
    RematImmutableGlobal
    RematMemoryDirectory
    RematTableDirectory
    RematGlobalDirectory
    RematCanonicalTypeID
)
```

Recipes must have fixed maximum depth, initially two.

A value may be evicted without a store when:

```text
rematerialization cost
<
store + future reload + frame growth cost
```

### Initial safe recipes

* Small integer constant.
* Zero.
* Sign/zero extension.
* `base + constant`.
* `base + index × scale + constant`.
* Immutable numeric global.
* Instance-stable directory pointer.
* Canonical type/layout ID.

### Exclusions

Do not rematerialize:

* Mutable memory loads.
* Trapping operations.
* Division/remainder.
* Atomics.
* GC handle resolution across collection.
* Mutable globals.
* Uncontracted host values.
* FP arithmetic where repeated evaluation could change Wago’s chosen observable NaN behavior.

## 5.3 Definite-assignment local initialization

During the existing summary scan, determine which declared locals may be read before a definite write.

At function entry:

```text
zero only may-read-before-write locals
```

Use structured bitsets:

```text
definitely written = intersection at merges
read before write   = union at merges
```

This removes prologue stores, reduces touched frame pages, and creates more frame-elision candidates.

## 5.4 Lazy parameter homing

Incoming parameters should begin as virtual locations backed by argument registers:

```text
stIncomingArg
```

Only store a parameter to its canonical frame slot when:

* Its register is needed for another value.
* It remains live across a clobbering call.
* A structured merge requires canonical storage.
* It must be published as a GC root.
* EH or host reentry requires a stable frame representation.

A parameter consumed once before any boundary should never touch the frame.

## 5.5 Immutable numeric global folding

Defined immutable numeric globals whose initializer is compile-time constant should become ordinary Valent constants.

Do not fold imported globals or reference values whose instance identity matters.

## 5.6 Generalize result sinking

Extend existing direct sinks to:

```text
call → integer local
call → FP local
call → vector local
call → reference local
call → global.set
call → memory store
call → return
call → next call argument
```

Do not force a call result through a canonical spill slot when its first consumer can use the ABI result register directly.

## Phase 1 exit criteria

```text
ordinary compilation allocation-neutral
facts survive positive cases and disappear on every near miss
random full-width differential tests pass
parameter-home and local-zero stores fall materially
extension/mask instructions fall on real corpus functions
no new ordinary spills
```

---

# 6. Phase 2 — regional register residency 2.0

This is the largest likely compiler-side execution win.

Current AMD64 regional residency is intentionally narrow: call-free, control-free, no EH, bounded body/local counts, integer locals, and at most nine active cache registers.

Expand it in five controlled steps.

## 6.1 Improve current eviction before broadening admission

Current selection is based largely on static local score. Replace it with a bounded next-use model.

Eviction order:

1. Dead local.
2. Local overwritten before next read.
3. Rematerializable local.
4. Clean local already synchronized to its frame slot.
5. Farthest next use.
6. Lowest loop-weighted remaining use density.
7. Cheapest reload.
8. Lowest target encoding benefit.

Do not cache:

* Known zero/constant values that are cheap to rematerialize.
* A local with one imminent final use.
* A local whose memory form already folds efficiently on AMD64.
* A local whose residency introduces a fixed-register conflict.
* A local whose register prevents a more valuable vector or address value.

## 6.2 Add FP and vector region banks

Maintain independent admission and pressure for:

```text
GP local cache
FP scalar local cache
v128 local cache
```

A function should be able to use GP regional residency without consuming vector registers, and vice versa.

Prioritize vector locals that are:

* Loop-carried.
* Used by repeated arithmetic/shuffle chains.
* Expensive to reload.
* Transferable directly into a vector result or store.

## 6.3 Multiple straight-line regions

Divide a function at hard barriers:

```text
call
control merge
safepoint
EH edge
atomic/fence
memory.grow
opaque plugin operation
```

The summary scan records only the top bounded set of region candidates:

```go
type RegionHint struct {
    StartPC      uint32
    EndPC        uint32
    WeightedUses uint32
    Flags        uint16
}
```

A call-making function can then have:

```text
uncached prefix
hot cached region
call
second cached region
return
```

without attempting to maintain arbitrary state across the call.

## 6.4 Simple loop residency

Admit one structured natural-loop class first:

```text
one loop header
one backedge
no nested loop
no EH
no indirect control
no host or foreign call
bounded exits
```

At loop entry:

* Load selected loop-carried locals once.
* Retain them across the backedge.
* Write dirty values only on exits where they remain live.
* Transfer dying values directly to consumers.

This uses the existing structured control stack, not a reconstructed CFG.

## 6.5 Small branch-local residency

For bounded `if` regions:

* Snapshot selected regional ownership.
* Allow each arm to use free registers independently.
* At merge, preserve only identical assignments.
* Synchronize divergent values through canonical storage.
* Intersect semantic facts.

Do not attempt arbitrary phi allocation.

## 6.6 Structured stack-slot overlay

Integrate lexical stack-slot reuse while building regional state.

Safe first cases:

```text
then scratch ↔ else scratch
sequential inlined-callee locals
call argument scratch ↔ call result scratch
mutually exclusive cold helper paths
region-specific temporary slots
```

At an `if`:

```text
save entry slot high-water
compile then
rewind
compile else using same slots
frame requirement = max(then, else)
```

Begin with non-reference scalar temporaries. Reference-slot overlay should land only after native root maps prove exact live meaning at every safepoint.

## Phase 2 target

For local-heavy targeted functions:

```text
local/frame memory traffic: 35–60% lower
targeted execution: 7–15% faster
whole remaining-gap suite: 1–3% faster
weighted full compile: <=3% slower
peak RSS: <=max(1 MiB, 2%)
```

---

# 7. Phase 3 — calls, ABI, and runtime boundaries

After frame traffic, call boundaries are the next major source of fixed work.

## 7.1 Compact function-effect summaries

The combined summary scan should produce:

```go
type FuncEffects uint32

const (
    EffectCallsHost FuncEffects = 1 << iota
    EffectCallsForeign
    EffectCallsIndirect
    EffectWritesMemory
    EffectGrowsMemory
    EffectWritesGlobals
    EffectWritesTables
    EffectAllocatesGC
    EffectCollectsGC
    EffectUsesEH
    EffectUsesAtomics
)
```

Compute transitive summaries over direct-call SCCs:

* Acyclic functions union direct-callee effects.
* Recursive SCCs union all member effects.
* Unknown indirect/host calls become conservative.
* Large edge sets may fall back to conservative summaries.

This is `O(functions + direct calls)` metadata, not function IR. Issue #316 already identifies call-effect summaries as part of the compiler-memory roadmap.

## 7.2 Preserve state across safe direct calls

A caller can retain:

| Callee lacks effect            | Caller may preserve                     |
| ------------------------------ | --------------------------------------- |
| `GrowsMemory`                  | memory-size cache                       |
| `WritesGlobals`                | numeric global facts/values             |
| `WritesTables`                 | immutable table/type facts              |
| `AllocatesGC` and `CollectsGC` | non-raw GC semantic facts               |
| `CallsHost`/`CallsForeign`     | lightweight execution context           |
| `UsesEH`                       | simpler callsite state                  |
| all observable effects         | pure expression/rematerialization state |

This should substantially reduce blanket invalidation.

## 7.3 Finite internal ABI classes

Do not create arbitrary per-function calling conventions initially.

Use a finite set:

```text
General
LeafScalar
LeafFP
LeafVector
TinyDirect
GCNonCollectingLeaf
```

Each class specifies:

* Argument registers.
* Result registers.
* Scratch registers.
* Preserved pin bank.
* Whether memory/global/GC context remains valid.
* Whether a frame, fence, or interrupt check is required.

The summary scan proposes a class. Compilation under a restricted class may retry once using `General` if register pressure exceeds the contract.

This captures the useful part of interprocedural register allocation without whole-program coloring.

## 7.4 Register ABI v2

Extend internal results to bounded banks:

```text
GP:     RAX, RDX / X0, X1
FP:     XMM0, XMM1 / V0, V1
vector: XMM0 / V0
reference: GP result with ownership metadata
```

Start with at most four scalar results and at most two per register bank. Wider signatures retain the existing fallback.

After a call, results remain symbolic physical locations rather than being immediately written to result slots.

## 7.5 Multi-result structured merges

Support a bounded merge descriptor:

```go
type MergeLocations struct {
    Count uint8
    Loc   [4]PhysicalLocation
}
```

Use parallel copies on edges. Fall back to slots for:

* Larger result sets.
* Complex mixed references.
* EH-sensitive shapes.
* Unresolvable cycles under the bounded move resolver.

## 7.6 Call-surviving pure expressions

A pending Valent expression may survive a call when it consists only of:

* Constants.
* Caller-local reads.
* Pure nontrapping integer operations.
* Immutable globals.
* Rematerializable context values.

Before the call, release any physical registers and retain only the bounded symbolic recipe.

Do not retain:

* Loads.
* Mutable globals.
* References requiring root publication.
* Division/remainder.
* Trapping conversions.
* FP operations with problematic recomputation semantics.

## 7.7 Native-byte- and pressure-aware inlining

Retain the existing bounded leaf inliner, but evaluate:

```text
removed:
  call instruction
  argument moves
  pre-call stores
  post-call reloads
  result moves
  callee frame/check work

added:
  callee instructions
  parameter binding
  local initialization
  caller frame growth
  caller spill pressure
  lost regional residency
  lost frame elision
```

Strongly favor inlining when it:

* Makes the caller call-free.
* Enables regional residency.
* Enables frame elision.
* Removes a single-use callee body.
* Exposes constants or exact GC facts.

Strongly penalize it when it:

* Disables a successful local cache.
* Introduces new spills.
* Duplicates a multi-caller body.
* Adds a call to a previously call-free function.

## 7.8 Prepared entry families

Generalize the successful direct prepared integer path through finite static trampolines:

```text
small scalar FP
small mixed GP/FP
v128 unary/binary
bounded multi-result
safe exact reference signatures
```

Do not dynamically generate one trampoline per signature.

Also add an explicit batch API for repeated tiny calls:

```text
enter foreign stack once
invoke N times
check trap between iterations
write caller-provided results
allocate nothing
```

## 7.9 Host effect classes

Explicit host registration can declare:

```text
may reenter
may grow memory
may mutate globals/tables
may allocate or collect GC
may block
numeric-only
```

The default is fully conservative.

A numeric, non-reentrant leaf host import can then use a narrow signature trampoline that:

* Saves only the required registers.
* Skips unnecessary root publication.
* Synchronizes only state the host can observe.
* Returns directly into internal ABI result registers.

Clang’s lowering benefits from carrying function memory effects, non-null facts, no-alias facts, and calling-convention information explicitly. Wago should use a much smaller semantic subset for internal and registered host calls.

## 7.10 Cross-instance specialization

Classify calls at instantiation:

```text
same instance
same runtime and same memory
same runtime, different memory
same collector domain
different collector domain
host wrapper
```

Prevalidate immutable identity and install:

* Direct target.
* Target basedata.
* Memory context.
* Collector-domain classification.
* Signature class.

Avoid rediscovering those properties at every call.

## Phase 3 target

```text
call-adjacent local stores/reloads: >=30% lower
small direct-call workloads: 10–25% faster
mixed/multi-result call workloads: 10–30% faster
ordinary call correctness unchanged
zero execution allocation retained
```

---

# 8. Phase 4 — generated selection and a bounded machine combiner

This is where the external compiler research should be incorporated systematically.

## 8.1 Wago rule schema

Create one source of truth for local target rules.

Each rule should declare:

```text
name
semantic input pattern
types
required value facts
effects and trap constraints
target features
physical-register constraints
output machine pattern
clobbers
cost
proof specification
```

Example:

```text
match:
  add(base, shift(index, k))

require:
  k in 1..3
  both values pure and nontrapping

target:
  scaled address instruction

cost:
  latency
  uops
  bytes
  register need
```

## 8.2 Generated decision trees

Cranelift’s ISLE compiles typed rewriting rules into ordinary efficient generated code, merging overlapping patterns into a shared decision tree.

A Wago-specific generator should output:

* Plain Go switches and conditionals.
* Rule IDs.
* Explain counters.
* Target predicates.
* Positive tests.
* Near-miss tests.
* Verification inputs.

No rule parser, map, reflection, or solver is present in production.

## 8.3 Offline formal verification

Model this after VeriISLE:

* Verify rule chains offline.
* Use bitvector semantics for integer rules.
* Model traps and state explicitly.
* Add target instruction semantics.
* Treat FP equivalence separately from bitwise integer equivalence.
* Use authoritative ISA semantics where practical.

VeriISLE demonstrates modular SMT verification of lowering rules and derives AArch64 semantics from Arm’s machine-readable specification.

Start with:

1. Pure integer rules.
2. Integer flags and carry.
3. Trapping integer operations.
4. SIMD bitwise rules.
5. Memory rules with one explicit region.
6. FP.
7. GC operations.

## 8.4 Fixed-capacity symbolic machine window

Use a reusable fixed array:

```go
const balancedMachineWindow = 24

type MachineOp struct {
    Op        uint16
    Dst       PhysicalLocation
    Src       [3]Operand
    Producer  [3]uint8
    Uses      uint8
    Effects   EffectMask
    Facts     ValueFacts
    WasmPC    uint32
}
```

Flush at:

```text
branch target
control merge
call
safepoint
trap-order barrier
EH edge
atomic/fence
opaque plugin output
capacity exhaustion
```

## 8.5 Producer-linked combination

Do not scan all pairs of machine operations.

For each consumer:

1. Follow direct producer links.
2. Explore at most four producer levels.
3. Query generated rules for that root operation.
4. Stage the replacement.
5. Validate effects, flags, physical constraints, and cost.
6. Commit transactionally.
7. Revisit only affected producers and users.

GCC’s combiner operates primarily on short, actual producer-consumer chains and substitutes earlier definitions into later users rather than performing arbitrary global search.

GCC’s newer late combine similarly stages definition substitution, validates all uses, and commits only after the complete transformation is legal.

## 8.6 Cost model

Use a small lexicographic cost, not one floating-point score:

```go
type SelectionCost struct {
    NewSpills    uint8
    CriticalPath uint8
    Uops         uint8
    Moves        uint8
    RegNeed      uint8
    Bytes        uint16
}
```

For Speed:

```text
new spills
critical path
uops/resource depth
moves
bytes
```

For Balanced:

```text
new spills
critical path/uops
moves
bytes
```

LLVM’s machine combiner explicitly checks critical-path and processor-resource depth rather than accepting every instruction-count reduction.

## 8.7 First rule families

Implement in this order:

1. Move-to-self and copy-chain deletion.
2. Cross-register-bank copy collapse.
3. Proven extension/mask elimination.
4. Load plus extension plus consumer.
5. Store-slot followed by load-slot forwarding.
6. Call-result to direct sink.
7. Compare/result/branch collapse.
8. Arithmetic-produced flags to branch/select.
9. Shift/add/address combinations.
10. Wide add/sub carry chains.
11. High-half multiply.
12. ARM64 address-update forms.
13. Target-feature-specific alternatives.

LLVM’s machine peephole work focuses on extensions, compares, foldable loads, and copy/bitcast chains—the same initial families.

## 8.8 Multi-consumer condition tokens

Retain one condition for at most three consumers:

```go
type ConditionToken struct {
    Producer  uint8
    Condition Cond
    Flags     FlagMask
    UsesLeft  uint8
}
```

Consumers can include:

* Branch.
* `select`.
* Boolean local.
* Boolean store.
* Carry/borrow chain.
* Wide comparison.

At an uncertain flags clobber, materialize the boolean.

GCC’s compare-elimination implementation uses a practical cap of three understood condition consumers, enough for common sign/equality and wide-comparison cases.

## 8.9 Bounded post-allocation renaming

Within the machine window, rename one physical def-use chain when it:

* Breaks a false dependency.
* Avoids partial-register hazards.
* Avoids a fixed-register conflict.
* Saves AMD64 REX prefixes without introducing moves.
* Enables ARM64 pair operations.
* Reduces cross-bank copies.

Never rename across calls or branch targets.

GCC’s register renamer builds local def-use chains and selects physical replacements based on conflicts and target preferences; Wago needs only a bounded local subset.

## Phase 4 target

```text
machine-window miss path: effectively neutral
no per-op allocation
ordinary window scratch fixed and reusable
rule fuel exhaustion falls back correctly
targeted sequences reduce instructions or dependency depth
whole remaining-gap suite improves without extra spills
```

---

# 9. Phase 5 — target-specific campaigns

After the shared infrastructure lands, optimize AMD64 and ARM64 according to their actual bottlenecks rather than forcing symmetry.

## 9.1 AMD64

Priority order:

### Fixed-register-aware selection

Improve operations involving:

```text
RAX/RDX multiply and divide
RCX variable shifts
BMI2 shifts and rotates
carry/borrow chains
byte-register constraints
```

Choose target features at compile time through an immutable feature bitset.

### Wide arithmetic

Recognize expanded wide-arithmetic idioms and lower directly to:

```text
ADD / ADC
SUB / SBB
MUL/IMUL high-half forms
BMI2 MULX where profitable and available
```

Keep carry as a condition token rather than an integer between limbs.

### Memory-operand selection

Use exact costs for:

```text
explicit load + ALU
versus
ALU with memory operand
```

Reject memory-destination read-modify-write forms when they lengthen a hot dependency chain, even if they are shorter.

### Physical-register encoding bias

Use low-register preference only as a final tie-breaker after:

```text
spill count
move count
critical path
```

### Bulk memory

Rebaseline and retune:

```text
tiny constant inline sequence
small dynamic vector loop
medium REP/vector path
large ERMS/FSRM path
backward overlap path
```

## 9.2 ARM64

### WasmGC native parity first

Complete issue #317 before adding broad new GC optimizer machinery:

* Final casts.
* Cast-plus-array-length.
* Final reference-array/struct reads.
* Scalar direct accesses.
* Nursery and remembered-object stores.
* Native constructor batches.
* Exact-reference facts.
* Late barrier-state lowering.

### Pre/post-index and pair operations

Recognize:

```text
load [p]; p += C
p += C; load [p]
load [p]; load [p+N]; p += 2N
```

Lower to:

```text
post-index load/store
pre-index load/store
LDP/STP
NEON chunk load/store
```

GCC’s auto-increment pass explicitly recognizes pre/post update forms around memory references and checks that the pointer update can be folded safely.

### Movemask consumer fusion

Do not materialize a complete scalar mask when the only consumer is:

```text
mask == 0
mask != 0
any bit set
all bits set
branch/select
```

Lower directly to a vector reduction/test and condition.

### Shuffle classifier

Classify static masks into:

```text
identity
splat
reverse
rotate/extract
zip/unzip
transpose
lane replace
table fallback
```

Select among:

```text
EXT
REV
ZIP
UZP
TRN
DUP
INS
TBL
```

### LSE and target features

Use:

* LSE atomics when available.
* LL/SC fallback otherwise.
* Dot-product/I8MM/FP16 only when semantics and features match.
* Target-specific cost profiles for Apple and server ARM cores.

## 9.3 SIMD/SWAR across both backends

Continue the existing successful strategy:

```text
offline sequence discovery
→ proof
→ benchmark
→ generated exact matcher
```

Do not run search online.

Prioritize:

* Movemask consumers.
* Reduction chains.
* Shuffle patterns.
* Widen/narrow chains.
* Byte packing/unpacking.
* High-half multiplication.
* Dot products.
* UTF/JSON state machines.
* Crypto rotations and lane permutations.

## 9.4 WasmGC runtime architecture

After ARM64 parity, proceed with:

1. Stable heap and handle backing.
2. Compact hot handle layout.
3. Faster exact type-kind metadata.
4. Fresh-object barrier elimination.
5. Batched allocation/initialization.
6. Shared native failure/refill tails.
7. Old-generation redesign only after telemetry proves it.

The compiler should preserve semantic GC facts across direct calls only when effect summaries prove no allocation or collection.

## Phase 5 target

Each campaign should have a distinct gate:

```text
AMD64 scalar/local campaign
ARM64 SIMD campaign
ARM64 GC campaign
bulk-memory campaign
```

Do not hide a regression in one architecture under an aggregate cross-architecture mean.

---

# 10. Phase 6 — narrow structured transformations

These borrow ideas from full compilers but use Wasm’s structured control to avoid a general CFG.

## 10.1 Tiny if-conversion

Admit only a bounded `if` whose arms are:

* Pure.
* Nontrapping.
* Call-free.
* Load-free initially.
* At most 8–12 Wasm operations total.
* Producing one scalar result or assigning the same local.

Lower to:

```text
AMD64 CMOVcc
ARM64 CSEL/FCSEL
vector blend when exact
```

Keep branches when one arm is strongly biased or the selected operations are expensive.

LLVM’s early if-conversion similarly uses strict block and dependency limits and excludes unsafe speculation.

## 10.2 Structured shrink wrapping

Target:

```text
entry
  cheap common fast path
  uncommon path needing frame/call/GC/EH setup
```

Initial form:

```text
frameless entry
fast checks
fast return

cold slow entry:
  allocate frame
  home required values
  perform call/helper work
  restore
  return
```

Admit only:

* One top-level early-return shape.
* No loop.
* No EH initially.
* No GC roots on the fast path.
* No frame-slot use on the fast path.
* Statistically or structurally cold slow path.

LLVM and GCC shrink wrapping place frame work only on paths that require it, but their general implementations rely on dominance and liveness. Wago should use a lexical structured subset.

## 10.3 Direct-call-chain stack-fence hoisting

For an acyclic direct-only call region:

1. Compute exact final frame sizes.
2. Compute maximum cumulative stack usage.
3. Check once at the externally addressable root.
4. Omit redundant internal checks.

Exclude:

* Recursive SCCs.
* Indirectly reachable functions.
* Exports/start functions below the root.
* Host reentry.
* Cross-instance entry.
* Unknown call targets.

## 10.4 Interrupt-poll coalescing

A tiny direct leaf may inherit a caller poll when it has:

* No loops.
* No calls.
* No unbounded bulk operation.
* Bounded instruction count.
* A caller poll within the documented cancellation budget.

Never remove polls from loops, recursion, or large leaves.

## 10.5 Exact structured loop hoisting

Hoist only operations proven invariant by the summary scan:

* Constant materialization.
* Immutable globals.
* Memory/table directory pointers.
* Stable context values.
* SIMD masks.
* Affine base setup.

Account for register pressure before hoisting. LLVM’s machine LICM likewise evaluates safety, pressure, latency, and block hotness rather than hoisting every invariant.

---

# 11. Phase 7 — profile guidance and research experiments

These should not block the main roadmap.

## 11.1 Optional offline profile input

Accept a profile keyed by module hash:

```text
function counts
call-edge counts
branch weights
loop weights
table target counts
```

The ordinary compiler remains one-pass. Profiles affect only:

* Regional effort.
* Inlining.
* Fallthrough choice.
* Function order.
* Alignment already supported by the completed size/layout work.
* Cold outlining/sharing.
* Optimization fuel.

Invalid or mismatched profiles are ignored.

## 11.2 Tiny equivalence sets at Valent sinks

Do not build a whole-function e-graph.

Allow at most:

```text
4 alternatives per node
rewrite depth 3
extraction fuel 96
```

Candidate alternatives:

```text
shift/add ↔ scaled address
mask/test ↔ bit-test
select ↔ branch/cmov/csel
affine address canonical forms
not/xor all-ones forms
```

Store an alternative only if it can plausibly beat the current form.

## 11.3 Two-lane or four-lane bounded SLP

Admission:

* Straight-line.
* Same operation and type.
* Adjacent or proven nonaliasing memory.
* No traps, calls, GC, atomics, or control.
* At most 16 scalar operations inspected.
* Two or four lanes only.

Instrument opportunities before implementation because optimized Wasm producers may already vectorize the useful cases.

## 11.4 Two-stage micro-pipelining

Do not implement Swing Modulo Scheduling.

Recognize only:

```text
innermost loop
single backedge
constant stride
prechecked memory range
no calls/GC/EH/atomics
body <= 8–12 operations
```

Generate at most:

```text
small prologue
two-stage kernel
small epilogue
```

Use a strict register-pressure and code-growth gate.

## 11.5 Implicit GC null checks

Guard/signal products could theoretically encode null handle resolution into a guaranteed faulting page and map the native fault PC to a Wasm null trap.

This is high risk. The experiment must prove:

* Exact null encoding.
* Stable reserved address range.
* Reliable fault classification.
* Correct source/trap attribution.
* No weakening of generation/type/bounds checks.
* No application to explicit-bounds embedded products.

LLVM’s implicit-null-check pass uses fault maps and a very small capped dependence search; Wago should be at least as conservative.

## 11.6 Software prefetch

Only consider:

* Hot innermost loops.
* Constant stride.
* Large working set.
* Sufficient trip count.
* PMU evidence of memory-latency stalls.
* No useful existing prefetch.

Keep it disabled by default until a representative workload wins consistently. LLVM similarly gates loop prefetching on target-provided distance, cache-line, and stride information.

## 11.7 Runtime tiering remains deferred

Do not add default runtime recompilation until:

* Local techniques plateau.
* Remaining time is concentrated in a small hot function set.
* A safe function-code replacement and reclamation protocol exists.
* Duplicate-code memory can be strictly budgeted.
* Offline profile compilation is insufficient.

---

# 12. Repository organization

A clean separation would be:

```text
railshot/shared/
    policy
    summaries
    effects
    value facts
    rematerialization
    optimization statistics

railshot/rules/
    semantic rule definitions
    generated matcher API
    common verification specifications

railshot/amd64/
    target costs
    target rule consumers
    machine window
    ABI classes
    target feature lowering

railshot/arm64/
    target costs
    target rule consumers
    machine window
    ABI classes
    target feature lowering

tools/
    rule generator
    rule verifier
    sequence harvester
    benchmark/profile joiner
```

The production backends should not import verifier or synthesis packages.

---

# 13. Recommended PR sequence

| Order | PR                                                    | Main result                                     |
| ----: | ----------------------------------------------------- | ----------------------------------------------- |
|     1 | Current performance rebaseline and causality counters | Establish actual remaining gaps                 |
|     2 | Complete #399                                         | Immutable, concurrent optimizer policy          |
|     3 | Compiler scratch subset from #316                     | Storage for facts, regions, effects, window     |
|     4 | Unified summary scan extensions                       | Definite assignment, effects, regions, next use |
|     5 | Complete #438                                         | Packed value provenance                         |
|     6 | Rematerialization recipes                             | Reduce spill/reload pressure                    |
|     7 | Definite-assignment zeroing and lazy parameter homing | Remove broad frame traffic                      |
|     8 | Generalized call/result sinks                         | Preserve ABI result registers                   |
|     9 | Existing regional cache next-use eviction             | Improve current admitted functions              |
|    10 | FP/vector regional residency                          | Extend register-bank coverage                   |
|    11 | Multiple straight-line regions                        | Support call-making functions                   |
|    12 | Simple loop residency                                 | Keep loop-carried locals in registers           |
|    13 | Call-effect summaries                                 | Preserve state across safe calls                |
|    14 | Internal ABI classes                                  | Reduce call preservation                        |
|    15 | Register ABI v2 and bounded multi-result merges       | Remove result-slot traffic                      |
|    16 | Rule schema, generator, and integer verifier          | Sustainable pattern infrastructure              |
|    17 | Machine-window identity path                          | Establish bounded symbolic emission             |
|    18 | Copy/extension/load/compare combines                  | First general local machine wins                |
|    19 | Multi-consumer condition tokens                       | Compare, branch, select, carry                  |
|    20 | Post-allocation register renaming                     | Break false dependencies and encoding waste     |
|    21 | ARM64 WasmGC parity from #317                         | Remove helper-bound ARM64 paths                 |
|    22 | ARM64 address-update, pair, and movemask campaign     | Target-specific execution gains                 |
|    23 | AMD64 fixed-register/wide-arithmetic campaign         | Crypto, bignum, hash gains                      |
|    24 | Prepared/host/cross-instance ABI specialization       | Reduce external boundary costs                  |
|    25 | Tiny if-conversion and structured shrink wrapping     | Optimize safe structured fast paths             |
|   26+ | Profile guidance and research experiments             | Only after evidence                             |

The rule-generator work can proceed in parallel with PRs 5–15, but the machine window should not begin until provenance and scratch ownership are stable.

---

# 14. Acceptance gates

## Per-PR default gate

```text
target workload:
    repeatable material improvement

whole corpus:
    no statistically significant regression beyond 0.5%

important individual row:
    >1.5% regression requires root cause

full compile weighted time:
    <=3% increase for ordinary local work
    <=5% for a major subsystem

compile B/op:
    <=2% increase

peak RSS:
    <=max(1 MiB, 2%)

execution allocations:
    unchanged on existing zero-allocation paths

native bytes:
    tracked to prevent I-cache regressions,
    even though size optimization is complete
```

## Cumulative core-program target

After phases 0–5:

```text
remaining-gap suite:
    8–15% additional execution improvement

whole broad corpus:
    1–5% additional geometric-mean improvement

weighted full compile:
    <=5–8% cumulative increase

peak compiler RSS:
    <=3% cumulative increase

steady-state execution allocations:
    unchanged
```

The remaining-gap suite is more important than forcing a large aggregate gain across rows where Wago is already ahead.

## Bounded-mechanism gate

Every bounded optimizer must have a test that:

1. Reaches its cap.
2. Falls back correctly.
3. Allocates no unbounded storage.
4. Produces deterministic code.
5. Does not retry indefinitely.
6. Preserves traps and source attribution.

## Rule gate

Every production rule requires:

```text
semantic rule source
generated matcher
proof or exhaustive-domain validation
positive test
single-precondition near-miss tests
architecture codegen assertion
corpus hit count
A/B benchmark
```

---

# 15. Explicit exclusions

This plan does not reopen:

* Branch relaxation.
* Final code compaction.
* Function alignment policy.
* Generated-code byte attribution.
* Code-size inlining objectives.
* Literal layout done as part of the output-size work.

It also rejects for the default compiler:

* Whole-function SSA.
* General CFG optimization.
* Full linear scan.
* Graph coloring.
* Full e-graph saturation.
* General LVN without new opportunity evidence.
* PRE.
* Trace scheduling.
* Full modulo scheduling.
* Online SMT or superoptimization.
* Unbounded dynamic tiering.
* Raising the Valent tree-height cap without a selector that proves a benefit.
* Indiscriminate permanent register pinning.
* Broad speculative bounds machinery without measured explicit-mode demand.

Cranelift’s final machine buffer is a useful model for recorded fixups and local branch transformations, but the completed Wago finalizer should remain bounded and separate from semantic optimization.

---

# 16. Immediate implementation order

The next four concrete changes should be:

1. **Complete immutable per-compilation optimizer state from #399.**
2. **Land the performance-causality ledger and current rebaseline.**
3. **Land the reusable scratch subset of #316 needed by facts and regions.**
4. **Implement #438 together with the first rematerialization recipes.**

After those, the highest-value performance sequence is:

```text
definite assignment + lazy parameter homing
        ↓
regional residency next-use
        ↓
simple loop and call-separated regions
        ↓
call effects + ABI classes
        ↓
register ABI v2
        ↓
generated machine combiner
        ↓
ARM64 GC/SIMD and AMD64 wide/fixed-register campaigns
```

That is the coherent path from Railshot’s current state to materially stronger execution quality without sacrificing its defining properties:

```text
single semantic pass
bounded compiler state
low peak memory
fast compilation
zero-allocation execution
deterministic conservative fallback
```

---

# 17. Implementation ledger

## 2026-08-15 — AMD64 bounded forward-merge register residency

The first Phase 2 branch-local residency slice landed for AMD64. The combined
function-summary scan records the first eight exact call/barrier-free `block`
and `if` body offsets for Balanced and Speed functions no larger than 4 KiB.
Lowering consults those marks without another bytecode walk and may keep dirty
pinned locals in their dedicated registers across the forward merge. Loops,
calls, `br_table`, GC/bulk/atomic prefixes, EH modules, summary-cap overflow,
larger bodies, and Size/Embedded objectives retain the canonical slot path.

The offsets occupy four extra words in the existing dense local-score backing,
not the per-function structure: `funcHints` remains 200 bytes, there is still
one module allocation, and no region operation allocates.

Native A/B results on a Ryzen 7 7800X3D:

```text
eight-region mixed if/block execution kernel:
    canonical: 11.50 ns/op, 372 native bytes, 0 B/op
    resident:   9.93 ns/op, 324 native bytes, 0 B/op
    delta:     about -13.7% time, -12.9% native bytes

64-module AMD64 corpus:
    merge stores: 724139 -> 689046 (-35093, -4.8%)
    merge reloads: 452566 -> 452625 (+59)
    native bytes: 81438930 -> 81304913 (-134017)

alternating comparison against the untouched if-only baseline
(regexmatch, SQLite, Ruby, esbuild; n=6 per mode):
    geomean time: -0.39%
    B/op: +0.45%
    allocs/op: +0.01% (rounding; no material change)

ordinary Ruby compile peak RSS, four one-shot processes:
    median: about 108442 KiB -> 108746 KiB
    delta:  +304 KiB (+0.28%)
```

The initial implementation exposed a no-`else` false-edge stub bug in SQLite:
the taken arm could fall through into a reload emitted only for the false edge.
The regression test reproduces the old `f(1) = 0` result and verifies that any
register-residency convergence code is skipped by the taken arm. Native SQLite
initialization and query execution now pass with the optimization enabled.

The rollback switch is:

```text
WAGO_AMD64_NO_MERGE_REG_RESIDENCY=1
```

### Rejected follow-up: per-block forward rescanning

Extending the same contract to call-free forward `block` targets increased the
64-module result to 31,434 fewer merge stores and 112,345 fewer native bytes,
and a mixed `if`/`block` kernel improved from 42.5 to 41.5 ns/op with zero
execution allocations. It was not retained because scanning forward from every
eligible block repeated work in nested control and failed the compile gate:

```text
alternating real-module compile sample
(regexmatch, SQLite, Ruby, esbuild; n=6 per mode):
    geomean time: +5.04%
    individual rows: +4.42% to +5.70%
    B/op: unchanged
    allocs/op: unchanged
```

Forward block residency should be reconsidered only after the combined summary
scan can mark admissible regions once. Reintroducing per-block reader scans is
not acceptable even though execution and native-code metrics improve.

### Rejected follow-up: simple-loop merge residency

A bounded prototype extended the combined region summary to admit a loop only
when its body contained no nested structured control, call, `br_table`,
`memory.grow`, or GC/bulk/atomic prefix. The loop header and backedge then used
the same register-resident merge contract as forward regions. This added no
summary storage and retained conservative fallback for every near miss.

The prototype was correct on native AMD64 and recorded two resident-local hits,
but it did not change the emitted code or the execution result:

```text
Ryzen 7 7800X3D, 1000-iteration integer loop, 10 samples:
    canonical: 227.5-228.4 ns/op, 174 native bytes, 0 B/op
    resident:  228.1-228.9 ns/op, 174 native bytes, 0 B/op
```

The existing loop-header reconciliation and control-boundary flushing already
produced the same effective code for this admitted shape. The prototype was
therefore removed rather than adding scan state without a material execution or
code-size gain. Simple-loop residency should be retried only with a distinct
fixed backedge contract that demonstrably removes dynamic loop-body traffic.

## 2026-08-15 — numeric inlined-callee slot overlay

Distinct numeric-only inlined callees now retain separate logical locals and
types while sharing one max-sized physical scratch-slot region. Inlined bodies
finish sequentially, and the existing result-realization boundary removes any
borrow from one callee's region before another splice reuses it. The transform
adds no table or per-operation allocation.

Reference locals and `v128` locals retain distinct regions. Size and Embedded
also retain the established layout because their post-lowering symbolic local
packer does not yet encode physical-slot aliases. The immutable per-compilation
selection bit and rollback environment variable are:

```text
inline-slot-overlay
WAGO_AMD64_NO_INLINE_SLOT_OVERLAY=1
WAGO_ARM64_NO_INLINE_SLOT_OVERLAY=1
```

The focused two-callee test reduces the caller frame by 16 bytes, preserves a
result from the first splice across the second splice, and produces identical
serial and parallel code. An `externref`-local near miss and the Size objective
both retain the old frame layout.

Native Ryzen 7 7800X3D corpus results:

```text
64-module Balanced compile, one compile per module:
    overlay hits: 3394
    cumulative function frame bytes: 1532440 -> 1504936 (-27504, -1.79%)
    native bytes:                    72783119 -> 72748399 (-34720, -0.05%)

alternating compile comparison
(regexmatch, SQLite, Ruby, esbuild; n=6 per mode, three compiles/sample):
    geomean time: -0.21%
    B/op:         unchanged
    allocs/op:    +0.01% (rounding; no material change)
```

The complete native AMD64 backend, focused race coverage, SQLite query,
execution corpus, and fuzz regression corpus pass with the overlay enabled.

ARM64 uses the same logical/physical split and conservative reference/vector
fallback. It has no post-lowering local-home packer, so all four objectives may
use the overlay. On Apple M4 Max the same corpus records 3,394 hits and reduces
cumulative frame bytes from 1,876,720 to 1,849,536 (-27,184, -1.45%); fixed-width
instruction encodings leave native bytes unchanged. Six alternating
regexmatch/SQLite/Ruby/esbuild comparisons move compile geomean +0.25% with no
significant individual row, while B/op and allocations remain unchanged.

## 2026-08-15 — bounded integer-constant rematerialization across calls

The first call-surviving recipe now retains one topmost `i32` or
`i64` constant below a nonzero-argument register-ABI call. The ordinary partial
flush reserves the constant's canonical slot without emitting its store; after
the call rebuilds the below-argument stack, lowering reinstalls the exact
constant recipe. Its later consumer can select an immediate or rematerialize it
without a frame reload.

The mechanism is one fixed local descriptor, not a recipe slice. It admits only
the immediately adjacent below-call value and has no per-operation allocation.
Zero-argument calls, non-constant values, EH modules, disabled policy, host
paths, and tail transfers retain canonical flushing. Integer constants are not
GC roots, so the recipe never weakens root publication. Mixed GP/FP calls use
the same bounded contract.

The immutable option and rollback controls are:

```text
call-remat-const
WAGO_AMD64_NO_CALL_REMAT_CONST=1
WAGO_ARM64_NO_CALL_REMAT_CONST=1
```

An opt-in corpus probe was removed after establishing demand. The complete
corpus contained 788 AMD64 and 790 ARM64 integer constants below calls; 560 and
561 respectively were the admitted topmost value below a nonzero-argument
call.

Focused 32-call results:

```text
Ryzen 7 7800X3D, ten samples:
    stored:         about 46.5 ns/op, 1268 native bytes, 0 B/op
    rematerialized: about 41.2 ns/op,  916 native bytes, 0 B/op
    delta:          about -11.5% time, -27.8% native bytes

Apple M4 Max, ten samples:
    stored:         26.37 ns/op, 1258 native bytes, 0 B/op
    rematerialized: 26.39 ns/op,  908 native bytes, 0 B/op
    delta:          execution neutral, -27.8% native bytes
```

Balanced 64-module code falls by 4,480 bytes on AMD64 and 2,768 bytes on
ARM64, with 560 and 561 retained recipes. Six alternating compile comparisons
over regexmatch, SQLite, Ruby, and esbuild report +0.36% AMD64 and +0.19% ARM64
geomeans. B/op and allocation counts are materially unchanged. Focused tests
cover integer and mixed-bank execution, zero-argument and EH fallbacks,
disabled policy, and serial/parallel determinism. Both complete native backend
suites, focused race tests, SQLite, execution corpus, and fuzz regression corpus
pass.

## 2026-08-15 — bounded integer-local rematerialization across calls

The call-surviving recipe now also retains one topmost `i32` or `i64` local
read below a nonzero-argument register-ABI call. A frame-backed local remains a
symbolic frame reference. A dirty pinned local is first protected from AMD64's
bounded call-dead scan, then published by the ordinary call-preservation pass
and restored as a frame reference. A pin-preserving finite ABI class may retain
the borrowed register directly.

The mechanism reuses the constant recipe's single fixed descriptor. It does not
add a recipe slice, another Wasm scan, or per-operation allocation. EH,
zero-argument, host, wrapper, tail, reference, FP, vector, and non-adjacent
values retain the canonical operand-slot path. AMD64 also declines a clean
register local in a function using forward-merge residency; the executable
corpus exposed that independent state combination in recursive
`memory_tree.wasm`, and the conservative fallback preserves the established
operand snapshot rather than relying on a second optimizer's frame-copy state.

The immutable option and rollback controls are:

```text
call-remat-local
WAGO_AMD64_NO_CALL_REMAT_LOCAL=1
WAGO_ARM64_NO_CALL_REMAT_LOCAL=1
```

An opt-in demand probe, removed before commit, found 2,490 topmost AMD64 local
reads and 2,366 ARM64 local reads in the 64-module corpus. After all admission
checks, the production transform records:

```text
AMD64: 1,900 recipes, 81,253,241 -> 81,228,025 native bytes (-25,216)
ARM64: 2,900 recipes, 91,584,152 -> 91,555,144 native bytes (-29,008)
```

Focused 16-call results:

```text
Ryzen 7 7800X3D, eight samples:
    stored:         about 27.84 ns/op, 522 native bytes, 0 B/op
    rematerialized: about 23.12 ns/op, 362 native bytes, 0 B/op
    delta:          about -17.0% time, -30.7% native bytes

Apple M4 Max, eight samples:
    stored:         about 18.91 ns/op, 524 native bytes, 0 B/op
    rematerialized: about 18.90 ns/op, 396 native bytes, 0 B/op
    delta:          execution neutral, -24.4% native bytes
```

Six order-balanced compile comparisons over regexmatch, SQLite, Ruby, and
esbuild report a -0.00% AMD64 and +0.42% ARM64 geomean. B/op and allocation
counts are materially unchanged. Focused tests cover dirty-register
publication, frame-reference retention, overwrite-after-call semantics,
forward-merge fallback, disabled policy, and serial/parallel determinism. The
complete local repository suite, both native backend suites, focused race
tests, SQLite query, executable corpus, and fuzz regression corpus pass.

## 2026-08-15 — depth-one pure-expression rematerialization across calls

The next call-surviving recipe retains one topmost depth-one integer ALU tree
below a nonzero-argument register-ABI call. Admission requires an `i32` or
`i64` nontrapping add, subtract, multiply, or bitwise operation whose two leaves
are constants or frame-backed caller locals. Register leaves, nested trees,
division, remainder, comparisons, shifts, references, FP/vector operations,
EH, host/wrapper calls, zero-argument calls, and tail transfers keep the
canonical operand snapshot.

The call flush reserves the expression's result slot but leaves its three
existing Valent nodes untouched. After the below-argument stack is rebuilt, it
removes the generated top slot and splices the original leaf/leaf/operator
block back into the intrusive stack. This adds no nodes, copy, slice, scan, or
heap allocation, and it preserves the original producer links for ordinary
target selection after the call.

The immutable option and rollback controls are:

```text
call-remat-bin
WAGO_AMD64_NO_CALL_REMAT_BIN=1
WAGO_ARM64_NO_CALL_REMAT_BIN=1
```

A removed demand probe found 216 AMD64 and 218 ARM64 deferred integer ALU roots
immediately below calls. The strict depth-one/frame-leaf contract admits 67 and
152 respectively. Balanced corpus output changes by:

```text
AMD64: 81,228,025 -> 81,227,337 native bytes (-688)
ARM64: 91,555,144 -> 91,554,296 native bytes (-848)
```

Focused one-call results:

```text
Ryzen 7 7800X3D, eight samples:
    stored:         about 13.16 ns/op, 182 native bytes, 0 B/op
    rematerialized: about 12.80 ns/op, 175 native bytes, 0 B/op
    delta:          about -2.7% time, -3.8% native bytes

Apple M4 Max, eight samples:
    stored:         about 13.10 ns/op, 216 native bytes, 0 B/op
    rematerialized: about 12.63 ns/op, 208 native bytes, 0 B/op
    delta:          about -3.6% time, -3.7% native bytes
```

Six order-balanced compile comparisons over regexmatch, SQLite, Ruby, and
esbuild report +0.03% AMD64 and +0.33% ARM64 geomeans. B/op and allocation
counts are materially unchanged. Native tests cover enabled and disabled
execution, code reduction, serial/parallel determinism, register-leaf and
nested-tree fallbacks, and the fixed descriptor path. The complete repository,
both native backend suites, focused race tests, SQLite query, executable corpus,
and fuzz regression corpus pass.

### Rejected follow-up: shallow conversions and comparisons across calls

A temporary exact-shape probe tested whether the same fixed tree splice should
also retain integer conversions and comparisons. The broad corpus had 20
conversion roots on each architecture, but none had a frame-backed leaf after
ordinary lowering; every one would require adding register preservation or a
deeper recipe. Only 5 AMD64 and 13 ARM64 comparisons met the complete
frame-leaf contract.

The probe was removed. Eighteen comparison sites do not justify widening the
production rule, rollback semantics, and target test matrix. This family should
be reconsidered only if profiles show those sites are hot or a later bounded
recipe representation already pays the required complexity for another
measured use.

## 2026-08-15 — AMD64 direct-call result residency

AMD64 now retains direct internal integer results in `RAX`/`RDX` while it
reloads caller-local and pinned-global state. Those reload banks are disjoint
from the result registers, so single, pair, and mixed GP/FP results can remain
symbolic physical locations and flow directly into their consumers. Indirect,
host, disabled-policy, and below-call rematerialization paths keep the existing
copy fallback.

Self-recursive calls also retain the fallback. An initial broader admission
removed the same copies there but slowed `fib_rec(25)` by a repeatable 7.5% on
the Ryzen 7 7800X3D. The explicit self-edge exclusion restores the benchmark to
about 338 microseconds per invocation, matching the copied baseline, while
retaining more than 99% of the full-corpus opportunities. This is a measured
AMD64 dependency-chain exception, not a semantic restriction; a later bounded
post-allocation renamer may revisit it.

The immutable option and rollback control are:

```text
call-result-residency
WAGO_AMD64_NO_CALL_RESULT_RESIDENCY=1
```

Across the 64-module Balanced corpus, the final admission records:

```text
direct result residency hits: 93,814
register result moves:         158,809 -> 64,995 (-93,814, -59.1%)
native bytes:                  81,043,790 -> 80,759,867 (-283,923, -0.35%)
```

A focused 16-call chain remains execution-neutral within noise, reduces its
caller from 259 to 164 native bytes (-36.7%), and executes with zero
allocations. Six order-balanced compile comparisons over regexmatch, SQLite,
Ruby, and esbuild report a +0.29% time geomean; B/op and allocation counts are
materially unchanged.

Native tests cover single, pair, and mixed-bank execution, disabled-policy
fallback, self-recursive fallback, interaction with below-call
rematerialization, code reduction, and serial/parallel determinism. The full
local repository suite, native AMD64 backend, focused race tests, SQLite query,
executable corpus, and fuzz regression corpus pass.

## 2026-08-15 — AMD64 clean and farthest-next-use regional eviction

AMD64 regional integer residency now completes the next two bounded eviction
priorities after overwrite-before-read. When pressure requires a cache register,
it first prefers a clean local whose canonical frame value is already current.
Within the selected cleanliness class, it chooses the farthest exact next access
when the existing 64-operation copied-reader scan resolves every candidate.
Fuel exhaustion, malformed immediates, structured boundaries, pending memory
borrows, and partially resolved candidate sets retain static score selection.

The scan returns two register masks and sixteen one-byte distances by value. It
does not add persistent function state, another body walk, a slice, or a heap
allocation. The original overwrite proof consumes the same result, so eviction
still performs only one bounded lookahead. Locals remain subject to the existing
score threshold before either new tie-breaker can select them.

The exact 64-module Balanced corpus records:

```text
clean-victim changes:       79
farthest-next-use changes:  17
ordinary stores/reloads:    unchanged
native bytes:               80,759,867 -> 80,759,586 (-281)
```

The affected executable modules remain allocation-free. Six order-balanced
Ryzen 7 7800X3D samples report `blake-as.hashN` improving from a 734,687 to
727,579 ns/op median (-0.97%) and `blake-as-simd.hashN` from 533,658 to 532,633
ns/op (-0.19%). Ruby contains 88 of the 96 changed decisions but is compile-only
because the checked-in module requires its external host bridge.

Six order-balanced compile comparisons over regexmatch, SQLite, Ruby, and
esbuild report a -0.15% time geomean. B/op and allocation counts are materially
unchanged. Tests cover clean preference, farthest exact next use, unresolved
fuel fallback, overwrite-before-read, and deterministic bounded scanning. The
existing `WAGO_AMD64_INTERVAL_REGIONS=0` switch remains the full regional-cache
rollback path.

### Rejected follow-up: whole-function admission with call barriers

A bounded prototype admitted non-looping, control-free call-making functions to
the existing interval cache and canonicalized every active owner immediately
before each ordinary call. This was correct without a CFG or new storage: call
arguments borrowing a regional local were demoted to their canonical frame
read, and the cache restarted lazily after the call.

Only four functions in the 64-module corpus were admitted, all in Ruby. Across
45 call barriers they reduced call-preservation stores by 17 and reloads by 27,
added two ordinary stores, and reduced native code by 187 bytes. Ruby cannot be
executed in the corpus without its external host bridge, so those code-shape
changes are insufficient performance evidence.

A focused eight-call, twenty-local kernel exposed the missing admission rule:

```text
Ryzen 7 7800X3D, eight samples:
    fixed whole-function pins: 17.91 ns/op, 372 native bytes, 0 B/op
    barrier-flushed regions:   20.77 ns/op, 771 native bytes, 0 B/op
    delta:                     about +16% time, +107% native bytes
```

The prototype and benchmark were removed. Multiple call-separated regions must
be summary-ranked by useful work between barriers; treating every eligible
whole function as a region repeatedly reloads locals that fixed pins preserve
more cheaply.

### Rejected follow-up: packed top-two call-region summaries

A second bounded prototype extended the existing byte scan with one current
call-separated region and two ranked winners. Each region admitted at most nine
distinct locals, retained only locals used at least twice, packed its 16-bit
start/end offsets and local IDs into four words, and rejected control, loops,
tail calls, bulk operations, EH, implicit helper calls, and cap overflow. Eight
words per storage-eligible function lived in the capacity tail of the existing
last-use allocation, so `funcHints` did not grow and the scan allocated no
per-region storage.

The exact 64-module AMD64 corpus contains 31,320 functions, of which 1,288 meet
the existing regional body/local storage bounds. The intended minimum of eight
useful local accesses found no admissible region while reserving 41,216 bytes of
summary storage. Lowering the threshold to four found one region with two
locals in one Ruby function. Even a two-access threshold found only six regions
and seven retained locals across five Ruby functions; Ruby still lacks an
executable corpus host bridge.

The scanner, packed representation, query seam, cap tests, and corpus probe were
removed. This opportunity density does not justify permanent summary memory or
lowering state. Call-separated residency should remain deferred until profiles
identify executable hot regions or another required summary can carry the same
facts without incremental module storage.

## 2026-08-15 — AMD64 direct deferred call arguments

AMD64 integer register-ABI calls now give each deferred argument expression its
physical ABI register as a destination when that register is free, unreserved,
unpinned, and has no local-cache owner. The existing Valent condenser therefore
emits the expression directly into `RAX`, `RCX`, `RDX`, or the later argument
registers, and the bounded parallel-copy resolver sees an identity edge. An
occupied or fixed-role target, a nondeferred value, disabled policy, and every
pressure conflict retain ordinary materialization followed by the proven
parallel move. No scan, retained descriptor, retry, or compiler allocation is
added.

The immutable option and rollback control are:

```text
call-arg-direct
WAGO_AMD64_NO_CALL_ARG_DIRECT=1
```

Across the exact 64-module Balanced corpus, the rule records 39,788 selections
and removes 39,434 attributed integer-call moves. Total module-image bytes fall
from 80,956,932 to 80,891,624 (-65,308, -0.08%); no module grows. Script has
16,635 selections, followed by SQLite (5,750), Ruby (5,686), Esbuild (4,488),
and Regexmatch (3,611).

On a 16-call Ryzen 7 7800X3D fixture, five complete one-second samples improve
from a 22.79 to 22.28 ns/op median (-2.2%), reduce the caller from 291 to 259
native bytes, and remain at zero B/op and allocations. Six focused compile
samples improve from about 16.26 to 15.65 us/op (-3.7%) with 36 allocations
unchanged. Fixed-work Ruby, Esbuild, SQLite, and Regexmatch compile medians have
a favorable roughly -0.3% geomean; SQLite is the largest memory change at about
+1.25% B/op and +0.36% allocations, within the gate. JSON serialize and
deserialize remain timing-neutral at roughly 108.7 and 194.9 ns/op with zero
execution allocations.

Tests cover enabled and disabled execution, physical-register near misses,
move and byte reduction, serial/parallel determinism, and generated-schema
registration. The full local repository suite, native AMD64 backend, executable
benchmark corpus, and regression corpus pass. The remote full-repository run is
otherwise limited by its absent pinned `tests/spec-v3` checkout and one unrelated
plugin go.mod fixture failure.

## 2026-08-15 — reject symmetric ARM64 call-argument destinations

The AMD64 direct-argument rule was prototyped unchanged on ARM64, including the
same free-register, ownership, pin, reservation, and deterministic-fallback
checks. Native tests passed, and the current 36-module compile corpus showed
21,487 selections, removed exactly 21,487 attributed integer argument moves,
and reduced emitted function bytes from 80,879,768 to 80,803,436 (-76,332,
-0.09%). Fourteen modules shrank and none grew. The focused 64-call fixture fell
from 904 to 648 native bytes and retained zero execution allocations.

That result does not satisfy the execution gate. Eight one-second Apple M4 Max
samples were neutral to slightly unfavorable (40.62 versus 40.72 ns/op median),
and JSON deserialize repeatedly regressed by about 1% in both A/B orders while
remaining allocation-free. Focused compilation was roughly neutral and kept 39
allocations. The ARM64 implementation and option binding were therefore removed;
`call-arg-direct` remains AMD64-only. This is a concrete example of the plan's
rule that semantic opportunities may be shared while target instruction choices
remain architecture-specific.

## 2026-08-15 — call-result move causality

The opt-in call-traffic ledger now partitions every register-result copy into a
mutually exclusive cause: ordinary direct fallback, self-recursion, indirect
call, `local.set` sink, or mixed-bank fallback. The five fixed counters live only
in requested `CodegenStats`; ordinary compilation and execution gain no state,
allocation, scan, or branch. Their sum is tested against the existing headline
result-move count through backend fixtures and corpus measurement.

On the current AMD64 compile corpus, all 61,307 result moves are attributed:

```text
call -> local.set       52,171
indirect result          6,214
direct fallback          2,139
self-recursive             769
mixed-bank fallback         14
```

This rejects the prior aggregate inference that Ruby's 53,981 result moves were
mostly the deliberately retained recursive dependency break. The next bounded
AMD64 target is instead transferring a direct call result into a local without
an unconditional physical copy. On ARM64, the same corpus reports 6,286 result
moves: 6,236 indirect and only 50 local sinks, so that target remains AMD64-only.

## 2026-08-15 — reject dead direct-call local results

A bounded AMD64 prototype reused the existing copied Wasm reader and 64-op
`call-next-use` fuel to recognize `call -> local.set x` results overwritten
before any read. Proven-dead results were left in `RAX` and discarded, while a
read, control boundary, malformed immediate, or exhausted fuel retained the
current result-to-local move. It required no summary, allocation, or retained
machine state and passed native execution, near-miss, cap, and deterministic
parallel tests.

The synthetic 64-call fixture improved from 64.47 to 63.91 ns/op median (-0.9%)
on Ryzen 7 7800X3D at zero allocations. However, the exact AMD64 compile corpus
recorded zero selections and left all 52,171 local-sink result moves unchanged.
The implementation and tests were removed. Dead-result lookahead should not be
revisited without workload evidence; the real local-sink opportunity requires
physical ownership transfer or a finite ABI result class, not dead-store
elimination.

## 2026-08-15 — AMD64 immutable-table indirect result residency

The branch-free immutable local-table specialization now leaves one- and
two-integer call results in the internal ABI result registers instead of first
copying them to arbitrary registers. This is admitted only when the caller
cannot appear in any immutable table accepted by the module summary. A caller
that may be selected indirectly retains the existing copy, which preserves the
self-recursive dependency break; generic, branch-split, imported, mutable, and
otherwise unproven indirect calls retain their ordinary fallback as well.

The reachability fact occupies recovered padding in the existing AMD64
`funcHints`: narrowing the bounded resolver-site count to `uint32` leaves the
structure at 200 bytes. Marking walks only already-admitted immutable table
initializers and active elements, allocates no storage, and is skipped entirely
when `call-result-residency` is disabled. No retained CFG, live interval, retry,
or execution state is added.

Across the exact 64-module Balanced AMD64 corpus, the rule removes 2,751 of
6,214 attributed indirect result moves (44%). Total native code falls from
72,261,111 to 72,252,841 bytes (-8,270); four modules shrink and none grow. Ruby
contains 2,237 selections, Regexmatch 484, wasm3 29, and dispatch one.

On a 64-call Ryzen 7 7800X3D fixture, six one-second samples improve from a
97.08 to 96.46 ns/op median (about -0.6%), reduce the compiled function from
6,176 to 5,982 native bytes, and remain at zero B/op and allocations. Tests
cover polymorphic table dispatch, the self-capable fallback, enabled/disabled
move attribution, serial/parallel byte determinism, and a 64-call native
execution path. The full local repository suite, native AMD64 backend suite,
and benchmark module suite pass.

### Rejected follow-up: dead call-local round-trip forwarding

A bounded AMD64 prototype recognized
`call; local.set x; local.get x` and kept the call result on the operand stack
when the existing 64-operation copied-reader model proved that the shown get was
the local's final read before overwrite or function exit. Reads, structured
boundaries, malformed immediates, and fuel exhaustion retained the ordinary
result-to-local move. The implementation needed no summary or heap storage and
passed native execution, serial/parallel determinism, later-read, and fuel-cap
tests.

The exact 64-module AMD64 corpus recorded zero selections. The scanner and
lowering were removed. Eliminating the remaining `call -> local.set` moves needs
a finite result ABI or bounded physical ownership transfer; another dead-local
lookahead does not match the current workloads.

### Rejected follow-up: AMD64 straight-line local value facts

An AMD64 parity prototype retained upper-zero, boolean, and sign-extension facts
through straight-line `local.set`/`local.get` pairs, matching the existing ARM64
assignment-version model. It replaced the unused type byte in `localDef`, so the
per-local structure remained four bytes, and control-bearing functions kept the
conservative no-fact fallback.

The exact 64-module corpus recorded 9,862 fact-carrying local reads across 14
modules, led by Ruby (6,411), but a direct candidate-versus-parent compiler
comparison produced zero changed native bytes in every module. AMD64's typed
32-bit local loads and materialization already establish the useful machine
width at the measured consumers. The bookkeeping and tests were removed;
architecture-neutral facts do not require symmetric target retention when one
backend has no code-selection benefit.

### Rejected follow-up: dead pinned-local argument ownership

An AMD64 prototype reused the existing 64-operation post-call liveness proof to
pass a dirty pinned local directly into the argument parallel-copy resolver when
the local died at the call. This removed the early borrowed-local-to-scratch
copy and still skipped the proven-dead canonical store. Reads, control
boundaries, fuel exhaustion, disabled next-use policy, and live locals retained
the established path. No scan, summary, or compiler storage was added.

The exact 64-module corpus found 706 selections across 15 modules and reduced
native code by 1,961 bytes; every affected module shrank and none grew. The
native execution gate rejected it. On a 64-call Ryzen 7 7800X3D fixture, six
one-second samples regressed from a 67.83 to 72.50 ns/op median (about +6.9%)
despite shrinking the function from 1,701 to 1,253 bytes; both paths remained at
zero B/op and allocations. The earlier scratch copy breaks the pinned-local
dependency before call staging, while the shorter sequence moves that dependency
onto the late ABI copy. The rule and tests were removed. Future call-position
ownership work must cost dependency depth, not only moves and bytes.

### Deferred follow-up: nonzero-table directory caching

A temporary AMD64 causality counter recorded every table descriptor derivation
that loads the multi-table directory rather than table 0's direct basedata slot.
The exact 64-module corpus recorded zero events. The counter was removed; a
protected directory register or one-entry descriptor cache would add function
state and register pressure without serving a current workload. Reconsider only
with measured multi-table traffic.

### Deferred follow-up: host effect classes

The current AMD64 rebaseline initially showed Wago's ordinary host round trip at
about 478 ns/op versus wazero at about 405 ns/op. Isolating export lookup and the
ordinary invocation contract with `PrepareFunction` reduced the same Wago
Wasm-to-Go-host-to-Wasm round trip to a 396 ns/op median on the Ryzen 7 7800X3D,
with the same 96 B/op and two allocations. The synchronous host transition is
therefore not the measured deficit. The remaining ordinary-path cost provides
instance serialization, close admission, collector ownership, and re-entry
identity; it must not be removed based on a leaf-host benchmark. Explicit host
effects remain useful only after a workload shows call-adjacent compiler state
invalidation, rather than public invocation bookkeeping, to be dominant.

### Rejected follow-up: dead forward-merge stores

The existing 64-operation merge next-use scan was temporarily extended to omit
a dirty pinned-local store when the local was overwritten before any read after
the merge. It remained bounded and allocation-free, hit 31 times in five of the
64 exact corpus modules, and reduced their combined native output by 208 bytes.
A 32-site Ryzen 7 7800X3D benchmark was flat at about 39 ns/op in both modes,
with zero execution allocations. Because generated-size reduction is baseline
work and this produced no material execution gain, the rule and tests were
removed under the execution-first admission gate.

## 2026-08-15 — allocation-free GC invocation suspension

Synchronous host dispatch no longer returns an escaping closure when it releases
and later reacquires the Runtime GC invocation domains around arbitrary Go host
code. A small value lease now carries the instance, domain view, topology,
owner, and dynamic-domain flag and restores the identical lock/claim protocol
through a direct method call. The zero value is the no-op fallback for start and
construction callbacks, so the change adds no cache, goroutine, or retained
runtime state.

Allocation profiling attributed exactly one of the two host-round-trip
allocations to the old resume closure. On the Ryzen 7 7800X3D, eight-sample
medians changed by:

```text
ordinary Invoke:  475.5 -> 448.1 ns/op (-5.8%)
prepared Invoke:  395.7 -> 382.2 ns/op (-3.4%)
both paths:         96 -> 48 B/op
both paths:          2 -> 1 alloc/op
```

The suspension operation itself is covered by a zero-allocation regression
test, including release, reacquisition, and ownership restoration. The prepared
host benchmark is retained beside the ordinary path so future boundary changes
continue to separate export/invocation bookkeeping from the native host
transition.

## 2026-08-15 — explicit numeric leaf host class

`LeafHostFunc` adds the first explicit host effect class. Its callback receives
only the raw parameter and result slots: it has no `HostModule` capability and
is contractually numeric and non-reentrant. Instantiation rejects every
reference-bearing signature. Existing `HostFunc` remains the fully conservative
default for memory, reference, collection, and callback-scoped re-entry access.

Synchronous bindings use one finite two-function-word descriptor per imported
function. The leaf dispatch calls its typed function directly and never boxes a
48-byte callback-scoped `instanceHostModule` into an interface. Ordinary
bindings still create the expiring generation-stamped value, preserving the
retained-handle fail-closed contract. The `Instance` layout remains 872 bytes;
the descriptor replaces the prior function slice rather than adding fixed
instance state. Synchronous instances also stop building the legacy async host
map, removing two instantiation allocations and 248 bytes on the one-import
AMD64 fixture.

Round-trip medians are:

```text
Ryzen 7 7800X3D:
    ordinary: HostFunc 440.0 -> LeafHostFunc 346.9 ns/op (-21.2%)
    prepared: HostFunc 373.2 -> LeafHostFunc 287.0 ns/op (-23.1%)

Apple M4 Max:
    ordinary: HostFunc 278.2 -> LeafHostFunc 235.1 ns/op (-15.5%)
    prepared: HostFunc 241.8 -> LeafHostFunc 201.7 ns/op (-16.6%)

all LeafHostFunc execution paths: 0 B/op, 0 alloc/op
```

Direct, void, imported-function re-export, and table-indirect tests cover the
new binding. The legacy wrapper remains available for non-synchronous modules,
while the synchronous hot path stays reflection-free and allocation-free.

## 2026-08-15 — reject shared-dispatch leaf bypass

A prototype recognized `LeafHostFunc` in `dispatchSynchronousHostCall` and
called it directly after the required native-lease and GC-suspension protocol.
This removed one intermediate dispatcher call. On the Ryzen 7 7800X3D it
improved the prepared leaf median from 284.9 to 268.9 ns/op (-5.6%) and the
ordinary leaf median from 345.5 to 337.1 ns/op (-2.4%).

The recognition branch also occupied the shared capable-host path. Repeated
alternating AMD64 measurements moved the ordinary `HostFunc` median from 438.4
to 447.5 ns/op (+2.1%). ARM64 showed no repeatable prepared-leaf improvement
and a smaller general-host regression. The prototype was rejected. Any future
attempt must specialize dispatch at instantiation so a general-only instance
executes byte-for-byte-equivalent routing, while retaining parked-frame roots,
collector lease suspension, native lease release, panic restoration, and
cross-instance context restoration for leaf callbacks.
