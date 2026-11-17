# Combined correctness checkpoint

Built source49cbd085. No performance acceptance measurements. Control is the
qualified no-capture/mapped default-stack TinyGo adapter. Both candidates keep
mapping enabled (environment switch unset), tasks/conservative/opt2/defaultstack.

Go full AMD64+runtime default/B4/B8/checker4/checker8 passed at a87422c8.
Subsequent changes only add delayed-poll tests and fix an explanatory comment;
focused old+new safety tests pass B4/B8/checker4/checker8 at49cbd085.
Default focused experimental selection had no tests; full default suite did run.
Actual TinyGo full AMD64 test package compiled; all named mapped arena/root/
overflow/panic tests passed for B4/B8 at a87422c8. Actual TinyGo isolated public
package at49cbd085 passed both variants: production finalizer3modes×4rounds,
10 lifecycle/decode tests and2 scalar writer tests. Exact source-hash/exclusion
manifest retained. Full public test package was previously killed137; this is
not a full public TinyGo suite pass and no OOM cause is asserted.

Delayed tests append instrumentation to a private native copy, validate complete
instruction boundaries/rel32 reach, preserve flags/registers/stack, and return
to original poll TEST. Second and final visits after earlier successful polls
check exact visit counts, completed source prefixes, grouped trap attribution,
engine reuse and unreachable activation full completion. Fixture reproduces the
GEMM pair shape; full-artifact outer-pin/register-pressure cancellation is not
claimed. Real threaded cancellation is separate.

Exact full GEMM diagnostic, wago_codegenstats not profile, uses explicit
interruptible compilation and knownAMD64 features: disabled14 attempts/rejects;
B4/B8 emit1 actual adjacent region in function1,1 bounded poll, no budget change.
Native images retained,4862 bytes disabled vs5662 enabled. This artifact is not
issue1060's distinct Rust example. Diagnostics are not semantic execution proof.

Both final cold adapters separately passed all145 semantic oracles/protocol gates
(290 paired records each). Image inspection passed all145 each;140 unchanged and
5 changed: ADI,correlation,FDTD2d,GEMM,Jacobi2d. Exact image hashes/sizes and raw
compressed-file receipts retained. One-trial cold correctness records are NOT
acceptance compilation timings. RSS/execution regressions and joint goals remain
unmeasured for this combination. No claim of V8 parity or40% compile improvement.
