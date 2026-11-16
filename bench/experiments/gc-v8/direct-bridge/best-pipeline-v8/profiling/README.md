# Separate ABI3 CPU profiles

Run only in an explicitly released CPU window:

```
python3 run.py --binary /path/to/original/candidate.test --output-dir /path/to/profiles --go /path/to/go
```

The driver verifies the original best-pipeline binary SHA against the recorded primary gate, then profiles struct and bulk-copy separately. It uses explicit 2 MiB Eden and two 8 MiB old semispaces, 128 warmup calls (enough for actual copying-kernel collection), then 512 calls. These invocation counts aim for useful CPU samples, not a new performance comparison. Test-binary CPU profiling covers setup, compilation and warmup too, not just the invocation loop. Checksums, exact allocation counts and real collection checks remain active. Any elapsed samples printed by the probe are profiling-contaminated and must not be reported as benchmark timings.

Interpret Go flat/cumulative samples only as attribution clues. Native generated code may appear as unknown/unmapped PCs, may not unwind across JIT/Go boundaries, or may be sampled incompletely. Do not interpret absent collector/helper symbols as proof they cost nothing, and do not assign unknown samples to a specific JIT operation without additional evidence. No V8 profiles or matched pause/collector-efficiency claims are produced.
