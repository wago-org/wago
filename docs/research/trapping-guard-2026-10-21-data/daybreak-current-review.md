## Candidate 1 — `61ad5f96` vs `a13816b5`

**No concrete bug found in the net production delta.** The checked space byte selects its own descriptor, including Large; the indexed load with R8 as both base and destination reads the address before replacing R8. The existing range, extent, type, and trap guards remain in place.

**Test gap:** The new tests cover descriptor selection and corrupt metadata across spaces, but do not directly exercise dirty pinned-global restoration on a cold trap from each changed resolver site.

## Candidate 2 — `46a9e4e9` vs `c6db3230a`

**No concrete correctness bug found.** The plan uses the call site’s actual inline selection, performs the first full resolution before pinning the address, and releases the pin after the covered second read. The reviewed paths preserve the source call mask and mark a second reference result as a GC root.

**Admission gap:** [gc_trapping_guard.go](/home/dev/Projects/wago-gc-trapping-guard-pr/src/core/compiler/backend/railshot/amd64/gc_trapping_guard.go:149) accepts an empty or NOP-only true arm, despite the stated trap-only condition. That broadens admission; I did not find a resulting semantic failure because reuse occurs only on the else edge. Add an admission test or require an `unreachable` in the true arm. The tests also do not directly demonstrate successful reuse with cache registers R12–R14.

This was a read-only source review; I ran no builds or tests.