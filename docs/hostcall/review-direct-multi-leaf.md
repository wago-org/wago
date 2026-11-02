Reviewed candidate `844428e3a23bd54fe0c2c5e7ece7561847ce5e0d` against exact base `90f2b57da`.

- **[P2] Gate runners return success despite failed gates.** [run-gates.py:46](/home/dev/.t3/worktrees/wago/t3-05c70de9/evidence/hostcall-direct-distinct-i32/run-gates.py:46) records each subprocess status but reaches EOF without returning a nonzero status when a gate fails. [cross-compile.py:17](/home/dev/.t3/worktrees/wago/t3-05c70de9/evidence/hostcall-direct-distinct-i32/cross-compile.py:17) has the same issue. A caller relying on either script’s exit code receives success even after test/build failure. Preserve the manifests and continued execution, then exit nonzero if any recorded command failed. Existing JSON evidence retains the failures correctly.

No implementation correctness findings in the reviewed scope. Indexed identity, generic frame trampoline, rollback ordering, cache ownership, rejection gates, legacy arm64 handling, unchanged +192 ABI, and scheduling appear consistent with admission. The differential staging-marker assertion provides evidence of actual direct execution.

Limits: execution evidence covers Linux amd64; foreign targets were compile-only. arm64 behavior remains execution-unqualified, and RV64 distinct leaves deliberately remain generic. The recorded ordinary-root baseline exception remains explicit. Review was read-only; no tests, timings, CI, children, or excluded machinery used.

Orchestrator disposition: the P2 aggregate-exit findings were fixed in both candidate replay scripts. A contract test runs the actual runners against 50 all-success/failure/signal scenarios, retaining complete manifests while requiring nonzero aggregate status for any failure. Runtime source is unchanged by this correction.
