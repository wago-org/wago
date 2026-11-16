# ABI4 loop/copy experiment preparation

Final measured source must be pinned after checked-copy integration and qualification. No build or result is implied by this preparation.

Same-source binaries:
- `baseline.test`: `wago_gc_direct_fill,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc`
- `candidate.test`: the same tags plus `wago_gc_direct_trusted_loop,wago_gc_direct_copy`

Both use ABI4 and the same collector. The A/B isolates the combined loop/copy capability, not ABI4 versus ABI3. Build sequentially with `go test -p 1 -c` only after the coordinated CPU window opens.

First run `run.py --matched --binary-dir DIR --output-dir MATCHED_DIR --eden 524288 --audit`, then without `--audit`. This is three rotating processes per variant/workload with one five-call timed batch, retaining all six cases. Warmup is 100 calls (128 at 2 MiB) so copy workloads actually collect. It is a short diagnostic, not a V8 gate; preserve all samples and controls.

Then omit `--matched` for candidate versus V8: original 100 warmup calls, nine batches of 32 calls, three rotating rounds, all six workloads. Run primary 512 KiB and separately labeled 2 MiB sensitivity using the same candidate binary and distinct output directories. The 2 MiB audit alone uses 128 warmups; timed full-protocol warmup remains 100. Both old semispaces remain 8 MiB. Report exact counts, real collections and all per-workload target misses. V8 allocation behavior and heap policy are not equalized; this remains an end-to-end engine comparison, not collector-efficiency proof.
