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
traps, and post-trap recovery. The focused suite passes natively on ARM64 and
through Darwin's AMD64 execution path; native AMD64 timing remains pending
because the configured host was unreachable. Three one-second Apple M4 Max
samples improved integer pairs from a median 83.88 to 19.00 ns/op (-77.3%) and
FP pairs from 83.59 to 19.37 ns/op (-76.8%), all at zero B/op and allocations.
The existing one-result FP direct path remains at roughly 18.0 ns/op. A
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
absolute gate. Full repository and focused race tests pass. AMD64 correctness
passes under Rosetta and its Linux test binary cross-compiles; native AMD64
timing remains pending because `hub@hub` still times out before authentication.

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
