# Rooting/exception prerequisites

These topic hunks are prerequisites to the authorized 997/1026/1027 integration, not a global main update. All production and test hunks are transplanted unchanged from these single commits:

- #1025 `40c1301ac8fb7dc5af14dd55cb8c46d9d8c09164`: AMD64/ARM64 custom instruction lowering consumes its source GC call mask. Required to keep subsequent exact callsite maps aligned.
- #1029 `1bf6e33bcfc12a035214d2ac1033bf7727a77b0a`: AMD64/ARM64 tag-free catch_all_ref maps and real foreign payload regression. Required for scalar-import EH payload ownership regardless of local tag directory.
- #1028 `0f0dffb8f78fb0e1f54d0034049e40d491870051`: frontend borrowed-exception aggregate rejection and explicit owned-representation plumbing in API/RV64, with frontend, API feature and native regression tests. Native catch-frame pointers must not escape into collector aggregates.
- #1031 `27abf4470e1137c18233d246e05dee6fb76ef266`: AMD64 local exception aliases retain their record after operand drop; real scalar/GC payload rethrow regression. Required for consumed/dropped-alias qualification.

No #1015/#1016 validation/parser or #1023/#1024 recipe behavior is transplanted: these are independent main changes, not necessary to the selected root/EH hunks. #1022 remains collector-owned; this worker does not modify native collector files. Any triggered missing Tiny gray-publication prerequisite is reported to that owner.

Original source regressions remain intact. Focused gates and final combined integration results are recorded separately. No final INCLUDED disposition or combined-port approval is asserted. No benchmarks or timing samples are taken.

Focused frontend, AMD64 backend and Wago prerequisite tests pass ordinary, wagodebug and wago_regalloccheck (raw logs adjacent). Original Tiny GC payload alias/rethrow and tag-free catch tests pass without a new #1022 dependency in this scope. Nonhost execution and combined cluster gates remain pending.
