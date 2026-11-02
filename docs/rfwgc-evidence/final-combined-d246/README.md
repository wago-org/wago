# d246 superseded qualification checkpoint

Source: `d2464100f1945d38f9d1e25196855fe9699d9875`.

**INCOMPLETE / SUPERSEDED: 32 PASS, 185 NOT_RUN of 217 required gates.** No required gate is failed; two prior harness failures and three interrupted attempts remain preserved separately. No new source defect was observed. This is not full combined qualification or platform approval.

Passed in all three modes: full root, runtime-tagged CLI, nested installer, nested bench module, package discovery/compilation, CI policy, repair census, explicit RFW catalog, and reduced parent reference-store race. Full collector and full collector/runtime/Wago/integration race additionally passed in ordinary mode only. The other modes and all other unclosed gates remain explicitly unrun in final-summary.json.

Recovery verified immutable exports, source/dependency archives, tools, frozen execution drivers, and terminal raw-log hashes before reuse and again after execution. Successful same-pin suites were not repeated. Original failed/interrupted attempts remain intact. New receipts use a separately recorded owned home cache. Raw logs are losslessly compressed.

The ordinary full race completed naturally. A checkpoint-only stop marker prevented the next gate from starting; its resulting scheduler assertion was intentional, not a test failure. See recovery-batch-stop.json and recovery-checkpoint-stop.json. The marker was archived only after the process census confirmed zero owned processes. No active stop marker remains.

The user subsequently authorized new source `9ad40bce4a7397dbadf0b0528f2587ed59d7d441`. **Do not schedule further d246 qualification.** New work belongs in sibling final-combined-9ad with new exports/drivers/logs. Old continuation scripts are retained as historical recovery artifacts, not authorization to resume this pin.

Darwin/Windows execution and the separate native Darwin run are not qualified by this evidence. No production/expectation edits, performance measurements, lease, PR, push, subagents, or review. Evidence-only changes cannot affect shipped performance. Shared dirt, index contents, and submodules were not reset or repaired.
