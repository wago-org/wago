# Matched checked versus trusted direct reads

Build two probe binaries from this same source with `go test -p 1 -c`:

- `checked_refstore.test`: `-tags wago_gc_direct_refstore`
- `trusted_read.test`: `-tags wago_gc_direct_trusted`

Run `python3 run.py --binary-dir <directory> --output-dir <directory> --audit`, then omit `--audit` for timing. The driver uses 64 KiB Eden as the retained control and 512 KiB as the explicit primary policy, with 8 MiB per old semispace in both variants. It rotates launch order across three fresh-process rounds, each with three warmup and five checked timed invocations. It retains binary hashes, exact artifact checksums, allocation/collection counts and process memory.

This isolates trusted scalar reads from the same checked reference-store pipeline. It does not enable the subsequent trusted-store prototype, change collector defaults, or establish V8 parity. Nursery policies are reported separately, not pooled. The 512 KiB policy was selected before this comparison because the preceding sensitivity experiment retained major collections with moderate additional allocated memory; the 2 MiB short run's avoided majors warrant a separate longer study.
