# d246 recovery checkpoint

Immutable source: d2464100f1945d38f9d1e25196855fe9699d9875. Current required status: INCOMPLETE, 19 PASS / 198 NOT_RUN of 217. No Darwin or Windows pass inferred.

Recovery verified the fresh source archive hash, original source/tooling/baseline manifests, frozen tools/drivers, and all 28 terminal command raw-log hashes. 26 terminal attempts passed (including five tool-version commands and wrapper steps); two historical harness attempts failed. Three attempts are interrupted. Actual qualification processes were absent; the census process list contains only its own launching shell. Earlier ordinary root failed with accidental GOWORK=off; its corrected terminal replay passed. No successful same-pin gate needs repeating.

Receipt symlinks were materialized under this owned evidence directory. New run-v4/bootstrap-v4 drivers use the separately recorded owned home cache, preserve old drivers, and refuse duplicate directory names. Original immutable source remains in its recorded /tmp export. No production or expectation edits.

Next finite batch, from repository root:

```
python3 docs/rfwgc-evidence/final-combined-d246/bootstrap-v4.py docs/rfwgc-evidence/final-combined-d246/recovery-root-batch.py
```

This batch deliberately skips the verified ordinary root. It stops at its first failing command. Inspect any failure before further scheduling; do not rerun completed commands. After this batch, inspect full-plan.json steps 3 onward; run individual steps through bootstrap-v4.py only after checking terminal receipts. All 217 requirements remain in required-gates.json. Old report/ledger scripts require addition of run-v4.py to known receipt drivers before crediting new results. Preserve every attempt.

Evidence-only changes cannot affect shipped runtime/compiler performance. No benchmarks, timings, lease, push, PR, or delegation.
