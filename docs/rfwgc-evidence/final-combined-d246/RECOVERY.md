# d246 recovery checkpoint

Immutable source: d2464100f1945d38f9d1e25196855fe9699d9875. Initial recovered status: INCOMPLETE, 19 PASS / 198 NOT_RUN of 217. The terminal root/CLI/installer/nested batch now passes all three modes; current counts are in final-summary.json. No Darwin or Windows pass inferred.

Recovery verified the fresh source archive hash, original source/tooling/baseline manifests, frozen tools/drivers, and all 28 terminal command raw-log hashes. 26 terminal attempts passed (including five tool-version commands and wrapper steps); two historical harness attempts failed. Three attempts are interrupted. Actual qualification processes were absent; the census process list contains only its own launching shell. Earlier ordinary root failed with accidental GOWORK=off; its corrected terminal replay passed. No successful same-pin gate needs repeating.

Receipt symlinks were materialized under this owned evidence directory. New run-v4/bootstrap-v4 drivers use the separately recorded owned home cache, preserve old drivers, and refuse duplicate directory names. Original immutable source remains in its recorded /tmp export. No production or expectation edits.

Next finite batch after root completion, from repository root:

```
python3 docs/rfwgc-evidence/final-combined-d246/bootstrap-v4.py docs/rfwgc-evidence/final-combined-d246/recovery-next-batch.py supplement
```

The root batch deliberately skipped the verified ordinary root. All batches stop at their first failing command. Inspect any failure before further scheduling; do not rerun completed commands. recovery-next-batch.py contains the remaining finite batch names. Run exactly one at a time, inspect terminal receipts, regenerate report.py and recovery-ledger.py, and commit completed evidence. All 217 requirements remain in required-gates.json. Preserve every attempt. The old ledger-v3.py does not recognize run-v4.py; use recovery-ledger.py.

Evidence-only changes cannot affect shipped runtime/compiler performance. No benchmarks, timings, lease, push, PR, or delegation.
