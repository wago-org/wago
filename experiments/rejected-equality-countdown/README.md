# Rejected ARM64 equality countdown experiment

Rejected for production on 2026-10-11 UTC after measurements on this Apple M4 Max. No live compiler source, driver hook, knob or optimization-catalog entry is imported by this archive.

The mandatory-dead-counter form is semantically sound under its bounded decoded proof and passes 148 signal-mode exact corpus contracts, full backend/catalog/encoder diagnostics, and strict fixtures in both bounds modes. It changes 29 application images and three core images. However, LCS improves 5.71% while edit-distance regresses 2.56%; affected median execution is -.11% and compilation +2.76%. The padding mitigation preserves hot-loop header positions but worsens edit-distance to +3.57%. Pristine option-off compiler controls are also retained; polygon's +1.27% means zero unconditional cost was not established.

- [Final decision, proof and results](reports/equality-countdown-results.md)
- [Confirmed affected tradeoffs](reports/equality-countdown-restricted-followup-summary.md)
- [Failed padding mitigation](reports/equality-countdown-padding-summary.md)
- [Pristine source-off compiler costs](reports/equality-countdown-offcost-summary.md)
- [Earlier investigation](reports/equality-countdown-investigation.md)
- [Source-only patch from c74d21c1](patches/equality-countdown.patch)

`reports/` contains summaries. `raw/` retains measurements, diagnostic/oracle logs, failed invocations and native inventories; `raw/profiles/` retains profiler captures and their manifests. `raw/scripts/` preserves the original reproduction scripts and summarizer. The scripts use the original isolated-worktree paths and frozen temporary binaries.

`source/` contains the final helper and native test as `.go.disabled`; `source/padding/` preserves the rejected padding helper and associated control-file variant, also disabled. These files are evidence and are excluded from Go package discovery. Every heavy build, test, benchmark and profiler capture used the shared benchmark-lock wrapper.
