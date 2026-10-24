# Folded descriptor addressing: full compiler qualification

This candidate is not yet published or installed. It removes the held Old
preloads and branch; the net production delta is only a checked 32-bit shift
and two indexed descriptor loads at five existing AMD64 resolver sites.
Every space/ref/type/extent/header/trap guard stays in place. Large remains
an independently selected descriptor. Source prototype f7ebbe973 and evidence
baa8f5796 are in /home/dev/Projects/wago-gc-fold-only-pr.

Combined qualification 61ad5f96db6ecada86a3efd7a1c471d796f083f9 is above
array-fusion qualification a13816b55. The only merge conflict preserves
castFailure(condA) and switches selection to emitGCSelectIndexedSpace;
PR1006 cold pin restoration stays intact. The comparison control is the
already-qualified installed array-fusion host, whose production source equals
installed dc961197a. No Old preloads, held type-fact/getter/collector experiments,
or diagnostic overlays are in either timed binary.

The three-variant, six-pattern, three-block micro retains all 108 samples.
Fold-only improves each pattern about 8%, including alternating and fixed-random
Old/Nursery chains. The refined Old branch is still faster for uniform Old and
the repeated random fixture, so branch entropy is not proved to explain the
held full-job result.

Daybreak Blue independently reviewed the net combined delta before any public
change and found no defects. Checked space is 1..4 before SHL4, both loads use the
same selected descriptor, and x86 effective addresses use old R8 before the
second load writes R8D. Gaps are direct selected-Base corruption, refresh races
outside the serialized ABI contract, and a standalone encoder alias test.

Normal focused tests pass in 28.057 seconds and checked+stats in 27.817 seconds.
The private diagnostic-free host builds in 8.198 seconds; binary SHA256 is
04d9a38b1122abba05ee7c8c71433cb1d6549e4caf0f308e9cc6fbb5558ac900.
Six serial Wago-only full-job samples pass. Control is 40.331539, 38.776936,
39.240592 seconds (median39.240592); folded-space is38.171805,38.273999,
37.673068 (median38.171805). The median reduction is2.724%, with all three
candidate samples below all three control samples. Each pair is lower by
5.35%,1.30%,3.99%. These are matched current-host results; do not compare the
absolute seconds across earlier matrices as host speed changes. All output
Wasm/cache hashes match, Wasm validates/main42 and required exact roots pass.
V8 is not remeasured. Full-suite/public checks remain before publication.

Publication preparation includes unchanged existing public safety/inlining
commits via merges of PR1006 and PR1007, preserving their original SHAs. This
new descriptor improvement remains a separate commit. No duplicate changes
are needed on the local combined stack, where those prerequisites are installed.
A current GPT-6 Sol review explicitly requested daybreak_blue through Codex CLI
and completed successfully with no concrete bug. Test gap: cold traps with dirty
pinned globals at every changed resolver are not directly covered by the new
descriptor fixture; existing PR1006 pin trap coverage is retained.

## Final joint qualification

Both public feature suites pass: normal26.388s and checked+stats27.944s.
The complete combined root suite passes normally154.411s and with the register
checker143.381s; the nested benchmark module passes checked28.225s. Exact three
pinned spec corpora setup took44.692s. Full commands/output/failures are retained
in data/gc-read-followups-qualification-2026-10-21/. No production changes followed
these checks. Both isolated full-job matrices passed and their effects are
reported separately. A final combined full-job pair is recorded separately.

The final combined serial integration pair is42.173588s control and39.366880s
candidate (6.655% lower). This is one directional pair, not a three-pair estimate.
Both outputs match the frozen Wasm/cache hashes, validate/main42 and have required
exact roots. No build or test overlaps these two timings. Raw commands/identities
are in data/wago-gc-read-combined-2026-10-21/. Relative improvements from the two
independent matrices are not added, and absolute times across matrices change
with host conditions. No V8 timing is repeated.
