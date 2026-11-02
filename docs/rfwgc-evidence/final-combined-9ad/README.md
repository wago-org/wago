# 9ad combined qualification — stopped on RR3

Source: `9ad40bce4a7397dbadf0b0528f2587ed59d7d441`.

**Candidate verdict: FAILED / INCOMPLETE.** Parent confirmed RR3 from review evidence commit `3b8732ad31098db5ea33a2f71a492d3fbcdcecf9`, `docs/rfwgc-evidence/rr-invocation-recheck/README.md`. RR1/RR2 are closed there; RR3 remains blocking. `gcInvocationSnapshot.unpin` always takes the topology writer, blocking return/cancellation/callback paths and producing a shared-memory notification wait cycle. The review reproduced RR3 in all three modes and race. This worker read and hashed that evidence; it did not start another review or rerun the known-blocked candidate.

Required gate ledger: **2 PASS / 215 NOT_RUN of 217**. The two passes are ordinary full-package discovery and compilation (`-run=^$`). They do not qualify runtime behavior. Five fresh tool-version receipts are supplemental provenance, not required-gate passes. No debug/checked execution, full root/race/corpus/spec/diagnostics/staticcheck, QEMU or TinyGo qualification was completed in this directory. Exact remaining gate names are in `remaining-gates.json`.

The parent stop arrived during ordinary compilation. The current command finished naturally with exit zero. The scheduler then exited 75 at its next-command checkpoint, before CI policy started. `CHECKPOINT.json` remains active to prevent further 9ad execution. No process was killed and no executing driver was edited. Final `process-drain.json` confirms zero owned processes. **Await a repaired source pin; do not resume qualification on 9ad.** A new pin requires its own immutable export, driver identities and receipts. Never relabel these passes.

## Provenance

- Fresh export/cache: `/home/dev/.cache/wago-final-9ad-zlt7btm6`.
- Source archive SHA-256: `1a162529c700a309778bcaf2538b5da00604577fea9cca6d17ba55093a617cd0`.
- Execution `run.py` SHA-256: `cdbe1c3765b0bd9e78db88d19319ba3451f29cf5b28cc5493cad815e9cdc3430`.
- All 4,009 tracked `src` blobs match the source commit. Ten RR tests were added to the driver census; source bodies/assertions were not edited.
- Original exports, dependency content, frozen drivers, tools and every completed command raw-log hash passed final integrity checks. Raw logs are losslessly compressed. `source-identity.json`, `drivers-frozen.json`, `integrity-latest.json` and receipt directories retain the proof.
- The separate 41-command implementation handoff at `7c4ab69aa3b8c1d3e38ee000ac8bf4642c2941f8` is background context; none of its results was imported into this ledger.
- Superseded d246 evidence is finalized separately at `2436595ac085c8193a8f39674b89846455dc38d9`: 32 required passes and 185 unrun gates, with retained historical harness failures/interrupted attempts. None was credited to 9ad.

Rebuild reporting only, without executing qualification: `report.py`, `ledger.py`, then `stopped-report.py`. The final step preserves the external blocking finding as candidate status without changing individual gate outcomes.

No production or expectation edits, timing/benchmark measurements, lease, PR, push, subagents, or new Claude review. Evidence-only files cannot affect shipped runtime/compiler performance. Shared source dirt, index contents outside the owned paths, and submodules were not reset. Native Darwin da19 and other platform execution remain separate; this evidence makes no native-platform qualification claim.
