# Bounded fixed shared-memory Go host segments

Retained `2cd322f0` on Apple M4 Max. Two reversed wasm.fyi comparisons against
`49145ddf` and the pinned SIMD w2c2-clang `f303ed27` reduce the exact owned opaque
thread callback workload by **52.28% and 50.23%**. Current latency is about
5.44 ms and remains **28.03x / 28.07x** the peer. This is progress, not parity.
The owned callback still invokes the same noinline Go helper, with normal
signature, store, lifetime, callback parking, and shared-memory publication checks.

The compiler proves every native segment reaches a real Go callback, return, or
trap. Admission requires one defined fixed-size shared memory, explicit bounds,
one import, numeric functions, and a small acyclic chain of guaranteed-yielding
local callees. Growable/imported memories, recursive or unbounded code, atomics,
bulk operations, indirect calls and unknown helpers fail closed. Native host
leaves are explicitly excluded because they do not yield to Go. This separate
certificate never enables the resource-free private callback bridge and is
discarded by artifact serialization.

The first prototype changed the unprepared entry only and measured no gain;
profiling showed ordinary Invoke still used its prepared host loop. Connecting
the same certificate to ordinary prepared entries and prepared sessions removed
those scheduler transitions. The diagnostic improved from 10.62 ms to
5.39–5.49 ms; the independent matched captures above determine retention.

Both runs contain four workloads, three source-pinned engines, four phases and
15 timing samples per successful phase: 24 captures / 96 phase rows, no collector
or resource failures. Native identity thread calls changed +0.71% / -0.12%,
compact imports -0.16% / +0.57%, and dispatch 0% / 0%. Native code sizes did not
change. Compact imports and native thread identity remain over 3x the peer.

Three alternating compiler-cost pairs, each with 100 passes over 218 Core 3 and
27 thread modules, show Core 3 elapsed -0.60%, allocation effectively unchanged,
RSS +1.11%; threads elapsed -4.95%, allocated bytes +0.93%, allocations +0.54%,
RSS +0.12%. Code size is unchanged. Timing/RSS differences are noisy and are not
claimed as compiler improvements.

Focused diagnostic checks, Core 3 execution and official atomic conformance
passed on ARM64. Native AMD64 on Remote passed the host/thread tests. Added
coverage exercises owned-reference retention, GC with one P, cancellation,
prepared sessions, shared-memory publication during callbacks, host panic/exit,
bounds traps after callbacks, codec fallback and rejected scheduler proofs.

The accompanying JSON contains source/contract/artifact audits, all matched
captures, compiler measurements and validation evidence. Captures remain private
pending a complete source snapshot; no production publication.
