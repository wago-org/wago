# Untimed age2 traffic metrics

Source bd4a4685 plus diagnostic-only accessor/probe. Tags SSE+refstruct+age2+age2_metrics; no promote-all cohort/shadow aliases, no weighted optimization. Explicit age2 true, total young2 MiB (active1 MiB/inactive1 MiB/cap512 KiB), old8 MiB each. Same six133-call workload probes with exact allocation/checksum/actual-GC/native-success reconciliation.

Snapshot fresh capacity/allocated, retained prefixes, first survivors, age-one promotion, cap-overflow promotion, full live young/old, old-direct allocation interval, remembered parents/edges/overflow slots. First survivors include retained+overflow, not additive promotion. Totals sum events, not unique lifetimes. Failures do not contribute successful totals. Extra scans invalidate timing comparisons. No elapsed-time benchmark output. Wait source review and explicit CPU release.
