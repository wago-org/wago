# Consolidated GC call optimization

The read-region work uses the bounded forwarding analysis from PR #1008 and the integer-inline path from PR #1007. Keep these call transformations in one review. The original forwarding commits and their tests remain in the merge history.

The branch starts at `8ab64d4b1dfcfbc80c0abfe72c3e8931e047dff9`, the GC trap-pin safety branch in PR #1011, above PR #948. Its tree already includes the original numeric-inline implementation. No ABI or codec version changes are part of this consolidation.

Private reference register calls (PR #1009) and type-test/first-field fusion (PR #1012) still serve different source patterns. They remain separate.

The full-job measurements use the installed combined GC stack at `073b2f61880e965d3c33ba83e32f72e9391a845d`. Results for that stack must not be presented as a measurement of this narrower public base. The read-region research note records the exact source delta, tests, review and timing evidence.

## Per-value checks prerequisite

The compiler scratch table also records successful test/first-field reads. Merge the test/field work from PR #1012 into PR #1007 so the shared checked-address producer has one review branch. The merge retains the test/field tests, safety parent, original trap checks, and its measured evidence. Reference-call ABI work remains separate. The scratch-table follow-up records its own correctness and performance results; no speed result is inferred from this merge.
