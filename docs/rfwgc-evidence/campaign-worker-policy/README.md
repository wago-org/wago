# Campaign worker speed directive — 2026-11-02

Joshua authorizes fast speed for every new repair, review and qualification worker for the remainder of the GC port campaign. Idle workers resume with fast speed before their next task. Do not interrupt an in-flight step merely to change speed or discard its work. The November4 target and all correctness/review/measurement gates are unchanged.

Apply the speed through orchestration settings, not merely the task prompt. New delegate settings belong in target.options; an idle child resumed with t3_thread_send uses its explicit speed=fast option. Verify the accepted setting when launching. If a provider/model cannot accept fast, report the worker and reason; do not silently claim a switch or substitute another model. A new review/recheck still requires a new delegate task.

At adoption, all identified campaign agent tasks have completed. Native GitHub run41481605659 remains separate CI execution; this directive does not alter its runner, deadlines, source/harness or correctness gates. No worker was terminated or restarted solely for speed. Historical completed worker settings are not relabeled.

This document records the authorization and future launch policy. It is not evidence that an unlaunched worker accepted a setting. No source change, benchmark, lease, PR or additional native dispatch occurred.
