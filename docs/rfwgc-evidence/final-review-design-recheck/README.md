# Design re-review of the DR-1 / DR-2 / DR-3 repairs (Claude)

Verdict: **NOT APPROVED.** Two verified blockers remain open on the latest
runtime pin. This report is advisory input to the parent. It makes no
inclusion, PR-readiness or measurement claim.

## Pins

| Role | Commit | Source |
| --- | --- | --- |
| Latest runtime pin (verdict target) | `d2464100f1945d38f9d1e25196855fe9699d9875` | immutable `git archive` |
| Original repair pin | `9180f09f7590d029bc90df2fa20c0decc590505a` | immutable `git archive` |
| Baseline | `9713878ab452848f7eb7549a90be94b938a898be` | immutable `git archive` |
| Original design report | `f8b0e1827` (`../final-review-design/README.md`) | read only |

- Between `9180` and `d246`, the only non-doc changes are `gc_conversion_store_other.go` (non-Linux), one new test, the CI workflow and the CI policy test. The Linux runtime sources are identical, so both pins are expected to behave the same on Linux, and they do.
- The shared HEAD (`b9433aa20` when this run started) was not used and gets no credit.
- Toolchain: go1.27.1 linux/amd64, at `/home/dev/.local/share/mise/installs/go/1.27.1`. `GOROOT` is pinned explicitly, and `GOTOOLCHAIN=local`, `GOPROXY=off`.

## Per-finding verdicts

| ID | Status at d246 | Evidence |
| --- | --- | --- |
| DR-1 consequence 1: an unrelated GC lease is blocked by a storage-only call | RESOLVED | `unrelatedLeaseErr=<nil>` for global and table, in all 3 modes and under race. At 5345 it was `context deadline exceeded` (prior log). |
| DR-1 consequence 2: a topology writer waits for a storage-only call | **OPEN: blocker RR-1** | `TestDesignRecheckStorageOnlyCallTopologyWriter` global and table fail at d246 and 9180 in all 3 modes and under race. The baseline control passes in all 3 modes. |
| DR-2: scalar cross-Runtime import from a storage-only producer | RESOLVED | The durable `TestDesignReviewExternStorageProducerCrossRuntimeImport` passes in all 3 modes and under race. Foreign relays are still fail-closed on converted slots (`TestDesignRecheckForeignRelayAfterLatePublisher` PASS). |
| DR-3: unsupported-platform converted publication fails open | Not re-executed. Source unchanged since the repair report. | Darwin and Windows execution is NOT_RUN, so no runner was available for an independent check. This is a coverage gap, not a correctness result. |
| New: a valid same-Runtime nested conversion store is rejected | **OPEN: blocker RR-2** | The nested clear (scalar-relay and storage-relay), the nested publish, and the callback test fail at d246 and 9180 in all 3 modes and under race. The baseline control passes. |
| Hypothesis: a host callback that changes publisher authority mismatches the resumed lease | REFUTED as an independent defect | The test fails only because of RR-2. Under the RR-2 counterfactual it passes, including its gate-leak and retired-domain unlink assertions. |

### RR-1: a storage-only guest call freezes the Runtime topology for its whole duration

**Repro.**
- Archive a pin. Copy `_probes/design_recheck_adversary_test.go` into `src/wago/`.
- Run `go test -count=1 [-tags=wagodebug|wago_regalloccheck] -run '^TestDesignRecheckStorageOnlyCallTopologyWriter$' -v ./src/wago`.
- Module S has no collector and only an `externref` global or table. S spins in guest code.
- Instantiating a GC module in the same Runtime then waits more than 2 s, and `instantiateBlocked=true`.
- The `none` control and the baseline (`TestDesignRecheckBaseStorageOnlyCallTopologyWriter`) do not block.

**Cause (d246).**
- `registerInstance` (`reference_store.go:2143-2144`) still marks any module that has externref storage as `dynamicInvocationDomains`. This comes from `compiledConversionStoreHooks`.
- `lockGCInvocationContext` (`reference_store.go:1580-1592`) takes `topology.lockContext(ctx, false)` for every dynamic instance. It holds that read lock until `lease.unlock`, so for the entire guest call.
- The repair README states this deliberately: "Storage-only instances still freeze topology across native execution".
- But the original DR-1 named this consequence explicitly, and it is a regression against the baseline.

**Spread (source-derived).**
- A same-Runtime scalar relay that imports from such a producer also becomes dynamic (`reference_store.go:2167-2171`), so the freeze spreads transitively.
- `gcTopologyGate` prefers writers: readers wait while `writers > 0` (`gc_admission.go:40`).
- So once one GC instantiation or Close is queued behind a long storage-only call, every later dynamic or storage-only invocation in that Runtime also stalls.
- This spread was not executed.

**Repair direction.** Do not hold the topology reader across native execution for a conversion-only view. For example, snapshot the domain set under the reader, then release it before native entry. Alternatively, make the conversion-only admission non-dynamic until the first conversion identity or container exists. In either case, adapt the authority checks that currently rely on a frozen topology.

### RR-2: nested cross-instance conversion stores check the callee's identity, not the root invocation

**Repro** (same procedure, at d246 or 9180):
- `-run '^TestDesignRecheck(NestedSameRuntimeClear|NestedPublish|CallbackChangesPublisherAuthority)$'`.
- Relay R calls `P.clear0`, a scalar import with no references in its signature. That clears a converted slot in the same Runtime. The call fails with `conversion store lacks pre-admitted invocation domains`.
- Relay R calls `Q.pub` (struct.new, then `extern.convert_any`, then a store into P's table). That fails the same way.
- A direct `P.clear0` invocation succeeds.
- All of these succeed at the baseline.
- These are valid programs: a same-Runtime scalar call chain is rejected. The rejection fails closed, and the receipt and storage stay intact, so it is a compatibility and correctness regression, not a safety hole.

**Cause (d246).**
- `publishConversionStore` and the bulk path check `domains.ownedBy(in.currentInvocationID())` (`gc_conversion_store_host.go:46` and `:214`).
- `in` is the instance whose code executes the helper: the callee, P or Q. `currentInvocationID()` reads the callee's `pluginState.invocationID`. That field is set only at public entry, on the root instance (`api.go`, `instance_entry.go:406`), so a callee reached by a native cross-instance call sees 0 or a stale ID.
- The root lease itself is complete. A same-Runtime relay that imports from a conversion-only producer inherits conversion-only admission (`reference_store.go:2167-2176`), so its view includes the publisher and claim domains.
- The collection path already uses the root's owner (`function_environment_collection_linux.go:62-68` reads `domain.invocationOwner`).

**Counterfactual (scratch archive only; not a proposed patch).**
- `attempt-.../logs/counterfactual.diff` replaces the callee identity with the lease owner of the first domain in the callee's view.
- With that change, the nested clear (both relays), the nested publish and the callback test pass in all 3 modes and under race.
- The foreign relay stays rejected, and the only remaining failure is RR-1.
- This identifies the identity source as the sole cause of RR-2. The real repair must carry the root invocation identity, for example through the native ctrl or activation root, as the GC struct helpers do. It must not trust whoever currently owns domain 0.

## Execution matrix (attempt-20261102T103623Z-claude)

All cells ran sequentially. Each log is non-empty, and every exit status is in `logs/exits.tsv`.

| Cell | ordinary | wagodebug | wago_regalloccheck | race (ordinary) |
| --- | --- | --- | --- | --- |
| d246 adversaries + durable DR tests | exit 1: 45 pass / 8 fail | same | same | exit 1, same set, 0 DATA RACE |
| 9180 adversaries | exit 1: 3 / 8 | same | same | - |
| baseline controls | exit 0: 9 / 0 | same | same | - |
| d246 + RR-2 counterfactual | exit 1: 50 / 3 (RR-1 only) | same | same | exit 1, RR-1 only, 0 DATA RACE |

Units:
- pass/fail are `--- PASS` / `--- FAIL` line counts, so subtests are included.
- The top-level d246 failures are the same 4 tests in every mode: StorageOnlyCallTopologyWriter, CallbackChangesPublisherAuthority, NestedSameRuntimeClear and NestedPublish.
- The 8 durable `TestDesignRepair*` / `TestDesignReview*` tests pass in every d246 mode.

## Runner history (preserved, uncredited where marked)

- `logs/` and `_probes/` at the top of this directory come from the cancelled earlier attempt. They are kept byte-for-byte.
  - Four of those logs are empty: control-base-9713878 wago_regalloccheck, durable-dr-9180 ordinary, recheck-5345 wago_regalloccheck and recheck-9180 wago_regalloccheck. They get no credit; the disk was full at that time.
  - Their non-empty results are superseded by the r2 matrix above.
- In this attempt, runner r1 (14 cells, labels without `-r2`) is an INFRA FAILURE. An inherited `GOROOT` pointed at go1.27.3 (`compile: version "go1.27.3" does not match go tool version "go1.27.1"`), so no test executed. See `logs/runner-r1-INFRA-FAILURE.txt` and the original runner `bin/run-r1.sh.orig`. These logs are kept and not counted.
- No historical log was deleted or overwritten. The runner refuses to overwrite.

## Sibling cross-check (read-only)

- **Slipstream:** `084be3d4bf70fb5337085ceddfab1bfa9db8b1c9` and current `dcb45b56386327a1f71fb039ed02e13562e1e2d7`.
  - It has no per-invocation domain lease and no topology reader/writer gate.
  - Imported root registration happens once, at import time (`src/jit.goose:279-305`).
  - Neither RR-1 nor RR-2 has an analogue there.
- **Starshine:** `1d6c0af47140299d8f3c01ed03060d279c43ee03` (starshine-master-base) is a model/validator boundary with no native runtime store. Not applicable.
- No sibling defect is asserted.

## Coverage vs correctness

- These are the source areas read for this verdict:
  - the admission and registration paths in `reference_store.go`
  - `gc_conversion_store{,_host}.go`
  - `gc_admission.go`
  - invocation-identity plumbing
  - the collection identity path
- These were not re-reviewed:
  - the rest of the repair diff
  - non-Linux adapters (DR-3)
  - RV64 and ARM64 execution
  - TinyGo
- No benchmarks or timings were taken. The 2 s waits are blocked/not-blocked bounds, not measurements.
- No production edits, push, PR or subagents. Scratch archives are in `/home/dev/.cache/wago-frr-recheck/attempt-20261102T103623Z-claude`.
