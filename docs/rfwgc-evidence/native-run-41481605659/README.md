# Native run41481605659: runner acquisition failure

**Darwin NOT_RUN in all three modes. This attempt never acquired a Mac runner.**
Source21d7be58952b64435110246f65d44e11046c5083; reviewed harness6d52c087f3136f0cb4d5457c733e1a92449df08b. Original dispatch.json remains unchanged. [GitHub run](https://github.com/wago-org/wago/actions/runs/41481605659).

| Darwin mode | Job | API result | Runner / steps | Recorded interval |
| --- | --- | --- | --- | --- |
| wagodebug |125401073301|cancelled|ID0, no name, zero steps|15m1s|
| wago_regalloccheck |125401073841|cancelled|ID0, no name, zero steps|15m1s|
| ordinary |125401073879|cancelled|ID0, no name, zero steps|15m1s|

Every check annotation states: “The job was not acquired by Runner of type hosted even after multiple attempts”. Each also records macOS ARM64 capacity-constraint notices. Thus the common interval is an unsuccessful hosted-runner acquisition window, not a test timeout, compiler failure, checkout/bootstrap failure or application execution. The API labels all three cancelled, even though the aggregate qualification summary fails. GitHub does not expose its internal acquisition retry policy here; this audit does not invent a configured15-minute workflow timeout. The reviewed jobs allow360 minutes after acquisition; increasing that limit would not address the observed cause.

GitHub exposes only three artifacts, all Windows. The complete available log ZIP contains Windows and summary logs, no Darwin job log. There can be no Darwin reservation/race/two-result/backend/ordinary closure from this attempt. Every historical failure and pending native requirement remains unchanged.

## Windows and custody

Each mode passes the exact six storage/admission top-level tests with zero failures/skips, source/harness identities exact and process/package-census exits0/0. This does not qualify Windows converted-GC JIT. All three original ZIP sizes and SHA256s match GitHub artifact metadata. Full job-log ZIP, run/jobs/check/annotation JSON and original manifests inside the artifacts are retained. The log ZIP has a locally recorded hash; GitHub provides no digest for it here. verify.py independently verifies the cancellation evidence, absence of Darwin logs, exact Windows terminal census, source/harness identity and artifact hashes. It does not independently attest every source-manifest blob or runner infrastructure.

## Deadline and repair cost

No source or harness repair is indicated by this attempt. There is no code-repair task to charge to these cancellations. The necessary next execution is the already-reviewed native qualification at the same pins once hosted Mac capacity is available, retaining all three modes and all gates. Queue delay and native execution duration remain unknown; the recorded15m1s is not a runtime estimate, billed usage or benchmark. The existing per-attempt ceiling remains1,173 job-minutes (1,080 Mac,90 Windows,3 summary); this audit neither spends nor extends it. Repeating only unsuccessful jobs may avoid duplicate Windows work, but no retry was dispatched by this audit. November4 remains at risk from external Mac capacity plus the remaining native/performance gates.

No new production finding, source edit, benchmark, measurement lease, push, PR or rerun. This was performed by the current parent turn, with no new worker launch or speed substitution. Evidence-only work cannot affect runtime performance. Original dispatch preserved.
