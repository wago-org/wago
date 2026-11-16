# Matched trusted reference-store experiment

Build two probe binaries from this same source with `go test -p 1 -c`:

- `trusted_read.test`: `-tags wago_gc_direct_trusted` (policy 1)
- `trusted_store.test`: `-tags wago_gc_direct_trusted_store` (policy 2)

Run `python3 run.py --binary-dir <directory> --output-dir <directory> --audit`, then omit `--audit` for timing. The driver retains 64 KiB Eden as control and 512 KiB as the explicit primary policy, with 8 MiB per old semispace in both variants. It rotates launch order across three fresh-process rounds, each with three warmup and five checked timed invocations.

This compares trusted reference stores against trusted reads on identical source, not against the older checked pipeline or V8. Policies remain experimental. Exact checksums, expected allocation counts, collection counters and process memory are retained. Do not pool nursery policies or hide noisy/regressing workloads behind an aggregate.
