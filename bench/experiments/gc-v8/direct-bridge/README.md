# Direct-offset bridge evidence

This compares experimental direct offsets with stable handles under the **same
helper-only compiler**, owned Runtime API, source revision, and configured
64 KiB nursery / 8 MiB old heap. Both disable `dead-gc-new`. It is not a native
fast-path comparison and not evidence of V8 parity.

Direct uses **two** 8 MiB old semispaces and fixed tracing metadata. Stable
handles use a different survivor/growth policy; equal config values do not imply
equal memory budgets or equal collection policy. Process peak RSS includes Go,
compilation, and runtime overhead, not just guest heap. Go memory snapshots and
collection/allocation counters are recorded. Direct copied/promoted-byte and
scan counters are currently unimplemented and must not be interpreted as zeros.

Build at the source revision under test, from the repository root:

```sh
go test -c -tags wago_gc_helper_only -o /tmp/direct-bridge.test ./src/wago
python3 bench/experiments/gc-v8/direct-bridge/run.py /tmp/direct-bridge.test \
  --output /tmp/direct-bridge-audit.json
```

Default run performs checksum/collection/memory audit only (no timed samples).
A later coordinated quiet window can use `--rounds 3 --samples 3 --batch 4`.
Inspect `timed_collections`; increase batches if the timed region has no GC.
Raw JSON retains failures rather than silently dropping unsuccessful workloads.
Artifacts and SHA256 checks come from the unchanged parent `cases.json`.
