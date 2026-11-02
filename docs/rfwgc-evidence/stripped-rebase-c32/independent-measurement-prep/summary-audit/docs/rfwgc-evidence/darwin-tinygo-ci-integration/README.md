# Existing CI Darwin TinyGo integration

Installed the reviewed supplement through the registered ci.yml workflow, with default-false darwin_tinygo_supplement. Source remains 7e41fafbbcd737ed1edce11b94b43b4eda134dcf; execution requires an explicit full harness SHA matching the workflow commit. No native execution or publication performed here.

Compared with the advisory proposal, ci-ok depends on both new jobs and the special summary does not depend on ci-ok. This preserves the established every-job aggregate contract without a dependency cycle. The special event never emits the required CI check name. Normal events retain their existing job bodies; the added jobs skip. Other qualification lanes are excluded; conflicting flags reject before execution. Dedicated run-ID concurrency does not cancel existing native runs. Maximum is 330 Mac job-minutes plus 3 summary minutes.

All three modes pass full CI policy and planner packages. Actionlint 1.7.7 passes. Tests additionally enforce supplement exclusion, summary failure handling and acyclic dependencies. The unchanged native harness, tool identities, 33/36/33 census and cross receipts remain in darwin-tinygo-supplement and its independent review.

Retained failed attempts: attempt-1 used quoted dependency names that the policy parser interpreted literally; normalized YAML list syntax. Attempt-2 exposed an overbroad text replacement in the summary Python expression; restored exact quoted key. Final tests and actionlint pass. No expectations or native oracles weakened.

This workflow-only change cannot affect shipped runtime performance. Fresh activation review is required before dispatch. Native Darwin TinyGo remains NOT_RUN. Cleanup 5b02 remains outside the execution pin and is separately reviewed; no final combined qualification claim.
