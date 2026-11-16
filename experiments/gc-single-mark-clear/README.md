# Single deferred mark clear candidate (unmeasured)

Base `35faf6c9`, branch `jairus/gc-single-mark-clear`. Remove only redundant entry
`clear(c.mark)` from direct promote-all, compact promote-all, compact age2 and
compact dedicated Eden. Keep each original unconditional `defer clear(c.mark)`.
No sparse indexed cleanup, captured queue count, per-object bookkeeping, policy,
allocation layout or memory-budget change is introduced.

## Ownership argument

- Both `New` constructors allocate private mark storage with `make([]uint64, ...)`,
  which starts zero. Derived constructors delegate to `New`.
- Repository search finds mark-bit writes only in these four collection routines.
  Other mentions are memory reporting and size comparison for separate shadowMark
  storage. Native views do not expose mark storage; payload APIs borrow arena only.
- Collection dispatch chooses one policy. Every mark write occurs after the
  unconditional deferred clear is installed. Ordinary errors (invalid references,
  capacity/queue OOM), successful returns and Go panic unwinding all execute it.
  Diagnostic callbacks before installing the defer do not touch this mark bitmap;
  completion diagnostics execute after the mark-clear defer due to LIFO ordering.
- Public core contract serializes all operations; there is no collector callback
  into user code or supported reentrant/concurrent collection. Unsupported races
  or memory corruption do not acquire correctness guarantees from an entry clear.
- Thus the private bitmap is zero between supported operations by induction. No
  legitimate API was found that deliberately leaves dirty mark state for the next
  collection. A fatal process termination does not require reusable collector state.

## Pending qualification

New package-private tests assert initially clean marks, invalid-edge Minor/Full
failure followed by repaired retry, partial-capacity Full OOM/retry, repeated
successful collections, and deliberately injected arena-bounds panic followed by
restoration/retry. The compact test covers all enabled policies and skips disabled
constructors. Panic injection tests only scratch cleanup, not general recovery
from arbitrary heap corruption. Existing failure/model suites remain required.

Initial source-stage checkpoint: no build, test or benchmark had run.
See QUALIFICATION.md for subsequent executed correctness results. Run normal/race,
compact age2/Eden coexistence, diagnostic combinations, TinyGo core and admitted
runtime suites before measuring. Then compare identical source/budgets/graphs,
retaining dense controls and all regressions. One less full bitmap clear is a
source-level work reduction, not a measured speedup or V8 parity claim.

## Current disposition

The candidate remains unpromoted. Matched runtime evidence is retained separately: fresh-forward-samebinary or gc-single-clear-matched, respectively. `candidate.patch` preserves the qualified source delta and tests for isolated reproduction; it is not applied to production here. Private execution prefixes in archived logs are omitted.
