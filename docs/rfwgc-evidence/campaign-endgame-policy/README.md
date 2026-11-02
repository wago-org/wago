# Owner endgame order — 2026-11-02

## Superseding owner order — early PR opening, 2026-11-02

Joshua explicitly directs opening a ready-for-review PR against upstream main NOW so Jairus can test, ahead of final qualification. This overrides the earlier CI-green → benchmark → PR order and perfection bar for opening only. It does not claim merge readiness or waive remaining tests/reviews/measurement. Push the committed port branch, link the PR to the owning thread, and create a fast-mode babysitter thread. Continue targeted PDFium rerun and remaining qualification, posting results to the PR as they land.

Opening disclosure: Darwin ordinary/debug native modes pass in full at pinned889b46c45, while checked race has one PDFium 60-second timeout and zero race reports. Targeted baseline/candidate run41526277406 is queued as last checked. Laptop benchmark proof has NOT run; earlier large numbers have NOT reproduced on final code and may only be described as historical measurements pending proof. No current performance delta is claimed. No tabled PR is closed by this opening order.

The earlier sequence below is retained as history and is superseded for PR opening.

1. Full CI qualification on the final port goes green. Notify main chat immediately. Main chat coordinates stopping other fleet work and quieting the laptop. Partial/scoped passes do not trigger this transition.
2. Before any PR opens, independently reproduce the earlier striking benchmark numbers on the laptop: final port branch against `origin/master`, identical earlier workloads/protocol, matched pairs and controls, under the standing measurement discipline. Fetch and pin the baseline SHA; pin the qualified candidate, exact tools, inputs, builds, protocol and claim identities. Review/reseal changed measurement artifacts rather than reusing historical executable seals.
3. Only after the benchmark proof, open the PR with fresh measured numbers. Existing review/perfection gates remain in force. If the numbers do not reproduce, notify main chat before opening anything. Never open a PR using stale numbers or characterize non-reproduction as success.

## Acquisition requirements retained

Independent measurer; single global lease; explicit fleet/laptop drain and owner coordination; both existing measurement locks reconciled; qualified thermal/throttle visibility; controls and matched-pair protocol; start each sample at <=60 C, no mid-sample pausing, discard/repeat overheating according to the standing protocol. No benchmark or timing acquisition before these gates. No stopping unrelated workers or acquiring a lease merely because this instruction was recorded.

The final-measurement-current seal (candidate041f, baselinea3c08365f, 349 cells) remains historical preparation. It is not the requested final-code vs origin/master proof and does not establish that the earlier striking-number workload/protocol has been identified. Identify the original report and exact claim/workload/protocol before acquisition. Preserve that evidence and any mismatch explicitly; no silent protocol substitution.

## Current gate

Full CI is not green: the checked native Darwin race suite hit TestPDFiumCorpus's hard-coded 60-second child deadline, with zero data-race reports (audit622a742e3). Ordinary/debug native modes pass. No benchmark, quiet-fleet transition, PR opening, or performance claim is authorized to begin by this record alone. Wednesday remains the target; no gate is waived.

Documentation only; this record cannot affect runtime performance.
