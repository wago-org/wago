# Campaign worker speed directive — 2026-11-02

Joshua authorizes fast speed for every GC-port-related thread launch or relaunch for the remainder of the campaign, including repair, review, qualification, the full review fan, orchestration and support work. This widened directive supersedes the earlier worker-lane-only scope. Idle threads resume with fast speed before their next task. Do not interrupt an in-flight step merely to change speed or discard its work. The November4 target and all correctness/review/measurement gates are unchanged.

Apply the speed through orchestration settings, not merely the task prompt. New delegate settings belong in target.options; any thread resumed with t3_thread_send uses its explicit speed=fast option. If the user explicitly requests a new top-level thread, t3_thread_launch also uses speed=fast; this directive alone does not authorize creating top-level threads. Verify the accepted setting when launching. If a provider/model cannot accept fast, report the worker and reason; do not silently claim a switch or substitute another model. A new review/recheck still requires a new delegate task.

At adoption, all identified campaign agent tasks have completed. Native GitHub run41481605659 remains separate CI execution; this directive does not alter its runner, deadlines, source/harness or correctness gates. No worker was terminated or restarted solely for speed. Historical completed worker settings are not relabeled.

This document records the authorization and future launch policy. It is not evidence that an unlaunched worker accepted a setting. No source change, benchmark, lease, PR or additional native dispatch occurred.

The expanded authorization applies prospectively at natural launch/relaunch boundaries. It does not assert that the currently running parent turn changed speed, nor that historical runs used fast mode. No mid-run interruption is required solely for speed.
