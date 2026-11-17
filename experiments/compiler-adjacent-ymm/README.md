# Interruptible adjacent YMM checked-tail experiment

SOURCE ONLY, UNQUALIFIED. No builds/tests/performance runs for this branch.
Basefc11f7a7 combined mapped/no-capture/B4/B8. Additional explicit tag
wago_experimental_adjacent_ymm is required. Old bounded tag alone remains XMM;
new tag alone cannot bypass bounded polling's interruptible admission gate.

Actual pinned-Go GEMM native image f944e2d58f68accac760cf04681c1a2dc156b4181602ce785b94f29186ca77ad
uses YMM at1170–117c,32-byte stream advances, then checked odd tail at1050.
CurrentB4 image77db5c31620268e6e184098c09e79d392d0ddd6a585a7a6c795c88c029e511f3
uses XMM. Width difference is observed, not a measured causal attribution.

Only existing AVX one-adjacent-pair contiguous proof is reused. After compiling
original fallback, require exact regionCheckedTailHomes; otherwise keep XMM.
No new planner/budget, FP reassociation, scalar recurrence or multi-pair admission.
All range/alias checks unchanged. Wide34-source prefix of35 has17groups, mandatory
finalwide poll, scalar exit publication, VZeroUpper and checkedtail entrypoll.
Original checked loop completes lastsource iteration. Tripcount slot30 and poll
slot31 are disjoint from wide publication26–29. No separately emitted widepair
tail is admitted for interruptible code.

B4 permits at most8source iterations between packed checks, B8at most16. This
is bounded work, not walltime or exact scalar cancellation cadence. Default and
old experimental builds retain previous contracts.

Source tests retain old XMM-specific cases by explicitly selecting XMM. NewYMM
cases require actual VZeroUpper in minimal GEMM image; cover1/2/3, batch boundaries,
35, delayed second/finalwide checks, checked-tail cancellation and unreachable
activation completion/reuse. Checked-tail-disabled fallback must emit XMM and
preserve exact results. Existing TestWideCheckedTailHomes exhaustively rejects
register/state mismatches for256homes; predicate is used unchanged. Full artifact
codegenstats diagnostic additionally requires wide-adjacent and checked-tail
emission, never profilemode. These tests have NOT RUN.

Still required: focused/newtag/oldtag/default/B4/B8/checker tests; FPedge and
alias/OOB/wrap regressions, outer-context fullGEMM cancellation audit; actual
TinyGo root/lifecycle gates;145oracles/changedimages; matched controlled
execution, coldcompile and absoluteRSS. No performance acceptance claim.
