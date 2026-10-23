# Prefix spill scratch register (2026-10-21)

The combined local GC stack's complete register-check suite exposed a failure in
`TestFloatCompareAllRelationsPendingLoads`. Prefix spill relocation saved RAX,
used it to copy frame slots, and restored it. The new deferred-address checker
correctly observes the intermediate overwrite even though this bounded copy
sequence restores the value before using the pending address. Release execution
was correct in this case. Do not suppress the checker or discard its live facts.

Use `allocReg` for the copy scratch instead. The allocator respects pinned
arguments and materializes/spills an owned address before returning its register.
After allocation, recompute the staging end to include any new spill. Both
direct backward copies and disjoint staging use that scratch. This removes the
fixed save/load pair and its extra eight-byte slot in the focused fixtures.
It does not add a runtime assertion or change the checker contract.

The new regression uses an integer predicate and forty pending f32/f64 loads, so
it also reproduces on origin/main without the separate float-consumer PR. It
fails with parent production code under `wago_regalloccheck`; the fixed version
sums all forty distinct loaded values and checks the branch result (821).
Existing slot movement, permutation, vector overlap, root metadata, pinned RAX,
zero-allocation, and exact emitted-copy tests have updated slot/code expectations.
The focused checked suite passes. The complete checked compiler package passes
on origin/main in 7.329 seconds (7.051 seconds in the test package). The same
production overlay removes the combined stack's float diagnostic failure; its
remaining failures are the old tests' expected copy-byte/slot counts, which this
commit updates. Full checked compiler results are retained in local timing logs. No whole-workload speedup is claimed for this repair.
