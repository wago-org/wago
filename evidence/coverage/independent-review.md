# Independent review

Reviewer: separate /root/review agent; no implementation/publication edits.
Source reviewed: c57256f5e3b6448202e04b6f8a5d266d893453ef. Measured merge:
66dedc1decfe9061b60a3b42101400950915a7c3; final main inherited ARM64-only change.

Read-only source report:

> No blocker found. The fix correctly reports aligned opaque changes under
> configuration mismatch while keeping both completion flags false. The regression
> also checks that unknown mapped semantics can coexist with complete opaque
> comparison. Qualification is never restored after a mismatch. README and
> acceptance scope match this behavior. The focused recipe now includes the required
> checker tag. No production compiler/runtime paths changed, and git diff --check passed.

Documentation report: PR/report accurately distinguish static observations, synthetic
controls, draft/full CI, native ARM64 limits and failed/interrupted presubmits. Reviewer
requested provenance/measurement refresh before publication; these are incorporated.

## Final frozen evidence verdict

**KEEP the bounded diagnostic and publish as a draft. No source blocker found.**

Reviewed frozen source 66dedc1decfe9061b60a3b42101400950915a7c3. All seven saved
executable hashes match actual artifacts. Both fib captures reconstruct exactly
105 bytes with the recorded native hash: 51 mapped plus opaque ranges 24, 22 and 8.
Every raw benchmark row matches summary; medians, ranges, allocations and deltas are
correct. Equal comparisons improve 55–61%; capture adds 1,252 B and 22 allocations.
Noisy timings and unmeasured memory remain explicit.

Synthetic constant/raw controls retain changes with exit 0; unsupported mapped input
produces exit 3 without losing opaque observations. Independent final checked/profile
test passed in 0.033 s. Diff checks pass; no experiment changes to production paths
against final main. Validation accurately retains missing-spec-v3 failures, checked
benchmark pass, and ARM64 compile-only qualification. No full-CI pass is claimed.

Reviewer required one remaining reproduction control revision correction (c952→e4);
that correction is now made. No reviewer edits, benchmarks or publication actions.
Historical integration review/fixes remain in ../integration/.
