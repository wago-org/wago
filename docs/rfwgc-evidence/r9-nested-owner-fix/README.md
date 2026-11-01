# R9 nested function owner repair — implementation in progress

Inputs: main `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`, updated port
`45c83820fbc10ee31eaaa8437e12464955b2fb9d`, bounded failed ownership evidence
`d7e6f5dfc1d04fee003a2492b515482d9453300c`. Rooting qualification is separately
recorded at `5019bffa`; it does not repair this owner edge.

The first working slice makes the existing object shadow-edge, conditional-slot,
bounded tracing and reclaim-observer APIs available on Linux non-TinyGo hosts.
RV64 implementations and API contracts are preserved; unsupported builds keep
stubs. The existing collector regressions are now portable too. The parallel
worker owns the required pointer-free mark skip correction in `mark.go`: an
object with shadow edges must be scanned even if its payload has no GC fields.
This dependency was found by stopped checked ownership assertions; no invalid
payload or function code was accessed afterward.

`native-{ordinary,wagodebug,wago_regalloccheck}.log` record passing focused
ObjectTrace/ObjectEdges/ReclaimObserver tests against the shared integration,
including the parallel worker's two mark skip guards. These are not immutable
final-head qualification. No benchmark or timing workload was run.

Parallel marking declines collectors with an object-edge source. Parallel sweep
only discovers handles; `parallelCollector.run` joins its helpers before the
coordinator commits frees and calls observers. Runtime callbacks must preserve
this serialized contract. No collector/mark/parallel-owned file was edited by
this worker.

Read-only sibling check: Slipstream report `dcb45b563`, reviewed source
`82d546679`; staged merge `e1ef65d6b90e4bd6153423ea52ae12a74fc2974f` contains
`ef06c3710`'s conservative failed-runtime retention when another runtime has GC
regions. That explicitly awaits exact ABI2 tracing and does not establish our
required eventual release or cycle reclamation. It is not called a qualified
landed fix here. Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03`,
`docs/wiki/wasm-gc-core-boundary.md`, concerns model/codec/validator/authoring
layers and provides no equivalent native rollback ownership mechanism.

Host graph integration and expanded lifecycle gates are still in progress.
No INCLUDED-FIX disposition, full combined gate, RV64 execution, or performance
claim is made. All original STOP assertions remain mandatory before follow-up
collection, container access, or teardown.

## Host graph working slice

The host path now shares RV64's descriptor-to-environment mapping, aggregate
shadow edges, construction journal, joint object/owner mark and post-lock
reclaim journals. Platform-specific dependency proofs remain separate: RV64
uses its existing typed receipts; hosts classify installed deduplicated import
attachments and revalidate the common physical-root epoch under owner locks.
Unclassified roots remain external. Claimed dead host components consume import
units through idempotent transfer records before ordinary physical journals.

Host environments have conditional persistent slots. Ordinary or in-callback
collections keep these slots strong, because host frame maps do not enumerate
all raw function words. Joint host collection requires every graph member's
atomic invocation count to be zero and construction inactive, across all store
members (including noncollector import participants). Complete domain/native
leases prevent native execution during the mark. End-of-invocation, direct-entry
and construction boundaries request and drain pending work; terminal Close also
requests it. The floating-retention bound is the outermost store-wide quiescent
boundary, not an additional guest invocation. Unknown external roots and borrowed
owners remain strong; this does not claim arbitrary external ownership disappears.

The original bounded probes now pass, with a producer pin already present at the
publication checkpoint, zero public function tokens, no escaped function calls,
and eventual physical callback exactly once. Expanded ordinary tests cover32
combinations: struct/array, two-level nested aggregates, failed/successful start,
last-edge clear versus provider/producer cycle close, and default versus actual
region shift6 (checked collector state). Reentrant provider allocation and host
callback collection retain the producer. Eight repeated callback-drop/Close
rounds per collector policy retain ownership throughout the callback, reclaim at
the failed-start outer boundary, and leave no environment/closed-store graph.

Environment creation makes function-storage mutations allocating helpers. Host
root-plan admission/liveness and AMD64/ARM64 safepoint emission now classify
struct.set, array.set, array.fill and array.init_elem only for function storage.
Numeric/compact-reference/ordinary extern mutations remain nonallocating.
array.copy reuses existing edges and does not allocate an environment. Aggregate
scratch roots the destination and mixed constructor compact operands before
creating environments. The preexisting generic array.init_elem regression and
new CollectEveryAlloc mutation/constructor fixtures pass; missing-safepoint
checks were retained. RV64 allocation classification and receipts are unchanged.

Focused host tests passed ordinary, wagodebug and wago_regalloccheck. Linux
ARM64/RV64 test packages cross-compiled (`-exec=true` is compile-only). Broader
three-mode/corpus/race qualification is still pending; the first broad ordinary
run exposed the now-fixed array.init_elem safepoint and missing corpus setup.
Do not treat these development logs as final combined qualification.
