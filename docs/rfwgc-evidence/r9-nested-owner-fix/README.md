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

## Foundation follow-up and immutable qualification ledger

Production foundation `9573c6249` passed the scoped three-mode matrix, but its
full ordinary package gate **failed**: an importer-owned bare-provider proxy
was rejected by the physical-owner lookup, and unused function-field type
metadata caused four unexpected corpus link rejections. The stopped checked
lookup did not authorize access to escaped code. `full-ordinary.log` preserves
that failed foundation run. No full-green claim applies to9573c6249.

Follow-up `1059c88e8e97840c121b3e4bc4e9f44b4b0c9a0b` restricts host graph
activation to actual generic GC execution and recognizes a proven importer-owned
proxy whose ordinary installed attachment retains its bare foreign provider.
Public cross-store token admission is unchanged. It also fixes a separate
retention defect: floating Env roots are excluded from **all** quiescent joint
mark seeds, including the live producer's owner roots. Only traced aggregate
edges retain the Env after the outer boundary. Unreachable floating slots are
cleared and reused. Each collecting mutation test repeats eight times while its
producer stays open and requires zero Envs/active floating slots at each return
and a stable persistent-slot count.

The first immutable1059 attempt stopped at a backend tooling failure after full
ordinary and scoped race ordinary passed. See
`attempt-1059-node-shim-failure/README.md`: Node resolved through an untrusted
mise shim. That attempt remains **FAIL**, not a historical PASS. The replay pins
Node26.11.1 explicitly. `qualify.sh` now creates a distinct evidence directory
per run and rejects overwriting an explicit existing qualification directory.
Each successful result applies only to the exact source/provenance and command
in that run; do not infer a result from leftover flat-directory logs.

A further source-audit limitation remains: the post-boundary driver uses
nonblocking topology/domain/native lock acquisition. Contention leaves a
pending request, but an unrelated Runtime releasing the global native lock does
not itself guarantee a local wakeup. Current deterministic no-contention tests
do not establish eventual release for that case. This scheduling obligation
must be resolved and tested before claiming the complete H6 eventual-release
contract, even if1059's requested suites pass. H2 production is paused pending
the parent's immutable-foundation gate directive.

## Independent H2 preparatory transaction repair

`6a25f86e5` validates replacement overflow before clearing the previous checked
root, and checks the table entry representation before changing ownership.
Its bounded test saturates only ordinary receipt metadata, requests a rejected
replacement, and checks that the visible slot, old checked root, and both counts
are unchanged before any cleanup. The test does not inspect a released object.
`transaction-6a25f86e5/` records a separate immutable three-mode qualification;
1059 does not cover this later commit. This is **not** the H2 container lifetime
repair: surviving-container receipts, independent domain claims, conditional
converted roots, bulk publication hooks and default/actual-region end-to-end
gates remain outstanding.

Read-only H2 source comparison: Slipstream
`084be3d4bf70fb5337085ceddfab1bfa9db8b1c9` (merged by `1a4759e5c`)
registers collectors before the first semantic section even without types, and
registers imported table/global root regions before publication. Wago's host
opaque identity bridge additionally needs balanced per-container receipts; an
opaque word cannot be used as a compact root.

Attempt `attempt-1059-node-pinned-V9pwdK` completed all fifteen requested
commands with exit0: full src/wago, scoped race, backend packages, and ARM64/RV64
compile-only in each of the three modes. Its `SHA256SUMS` covers the archived
per-suite ledger and artifacts. The driver subsequently exited127 because this
worker edited the live script while Bash was executing the parsed loop; Bash
then resumed reading at an obsolete byte offset. `driver-status.txt` records
this orchestration failure separately. The driver is **not** called PASS. A
clean replay runs an unchanged script copy in a fresh directory; no H2
production progression is based on concealing this failure.

### Clean immutable1059 verdict

`attempt-1059-clean-driver/` is the authoritative clean replay. Its unchanged
script copy exited0, all fifteen requested commands exited0, and `SHA256SUMS`
covers the command files, logs, source/tool/corpus provenance, per-suite ledger
and driver status. Earlier failed attempts remain preserved separately.

| Suite at1059c88e8 | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full `./src/wago` including pinned corpus | PASS | PASS | PASS |
| Scoped ownership/lifecycle race command | PASS | PASS | PASS |
| Full AMD64/ARM64 backend and Wasm compiler packages | PASS | PASS | PASS |
| Linux ARM64 test-package cross-compile | PASS | PASS | PASS |
| Linux RV64 test-package cross-compile | PASS | PASS | PASS |

Cross-compiles use `-exec=true` and do not establish target execution. Race is
scoped to the exact recorded regex, not the entire package. This qualification
predates later Stage7 collector commits and the independent6a25 transaction
repair. The contention-wakeup obligation described above remains unresolved;
no final INCLUDED-FIX, combined H6/H2, joint TinyGo, or performance disposition
is inferred from these fifteen passes.
