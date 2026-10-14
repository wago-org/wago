# Retain immutable shared directories on Go host return

Retained runtime change e57e1232 against dde95c0e in two reversed wasm.fyi
comparisons on Apple M4 Max, with pinned SIMD w2c2-clang f303ed27. The exact owned
opaque callback retains its noinline Go helper and complete portal, signature,
store, lifetime, publication and migration semantics.

| Steady workload | Change, first / reverse | Current / peer, first / reverse |
| --- | --- | --- |
| Opaque thread callback | -20.31% / -19.73% | 22.75x / 23.02x |
| Identity thread callback | +3.56% / -0.97% | 1.80x / 1.73x |
| Ordinary compact imports | -0.52% / -0.43% | 2.31x / 2.33x |
| Minimal dependent callback loop | -0.84% / +0.50% | 1.94x / 3.00x |

Opaque callbacks now take 4.35–4.41 ms versus 5.46–5.49 ms. This adds a measured
approximately 20% gain to the earlier scheduler improvement, but remains far
from peer parity. The controls do not show persistent regressions. Code size is
unchanged for all selected workloads. All 24 immutable captures / 96 phase rows
passed exact source/artifact/contract/resource audits with 15 samples per phase
and no collector/resource failures.

The compiler and native ABI are unchanged. During directory construction the
runtime records whether every entry is a shared memory32 backing whose captured
size already equals its immutable grow limit. Crucially, the same size snapshot
both initializes the entry and proves its stability; a concurrent grower cannot
freeze an older extent. Stable entries then avoid refresh on ordinary callback
return. Growable or mixed directories retain refresh. Migration, publication,
context-version changes and collector-owned context still take the complete
restoration path. Ordinary instances acquire no new sidecar; the existing
indexed/shared-memory sidecar gains one Boolean.

The initial opaque profile measured refreshMemoryDirectory at 15.21% cumulative
CPU and parked lease resume at 22.94%; these overlap. After the change directory
refresh disappears from the hot profile and lease resume is 7.98% cumulative.
A separate diagnostic moved from 5.52 ms to 4.35 ms. The reversed wasm.fyi
measurements, rather than profile percentages, determine the speedup above.

New tests cover fixed and growable memories, mixed directories, fixed shared
pairs, memory64 exclusion, growth from another goroutine, GC, publication during
callbacks, and supported cross-instance callback entry. They run alongside owned
reference, cancellation, trap/exit, cross-instance, Core 3 and official atomic
diagnostics. ARM64 diagnostic validation passes and native AMD64 on Remote
passes the host/thread/cross-instance tests. No Hub workload was started.

The first growth fixture omitted export/grow operations, allowing the existing
compiler to reserve only its private minimum; exposing memories corrected that
assumption, and corrected contracts pass before and after the change. A trial
direct same-instance callback Invoke waited on its documented invocation gate;
the supported cross-instance entry replaced that fixture. Neither required a
production behavior change.

Three alternating compiler-cost pairs (100 passes, 218 Core 3 / 27 thread
modules) show allocation counts, allocated bytes and native code unchanged.
Core 3 elapsed -1.56%, RSS -1.73%; threads elapsed -2.56%, RSS -0.04%. These
short noisy differences are not claimed compiler improvements.

The JSON includes paired captures, compiler measurements, validation and profile
evidence. A complete e57e1232 snapshot is collecting with full retention audits.
If ready before the next two-hour local refresh, it supersedes the older queued
source before any receipt; otherwise it waits for the following window. No
production deployment or push occurred.
