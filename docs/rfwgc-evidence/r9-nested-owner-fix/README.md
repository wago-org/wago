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
