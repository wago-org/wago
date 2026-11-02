# H2 physical conversion receipt core

The bounded production core is implemented. **Overall H2 remains incomplete:**
public/backend imported/global/bulk hooks remain a separate task. No final H2,
INCLUDED-FIX, combined-head or measurement-readiness claim.

## Source and tests

Final immutable source/test pin:
`79db49daa1ea414d9c25e753e8ddd72ba0d6be78`.
Starting qualified source10c2006fb and evidence500eacbc8 were preserved.

- `b61de6ef1`: publisher-independent opaque identities, per-container receipts,
  registered conversion-domain lifetime, mirrored-table prepare/commit/rollback,
  and joint collection without a collector-owning Instance.
- `ae64b76c1`: canonical physical descriptors, alias/rollback/close-order/cycle/
  callback/contention tests. Its full ordinary footprint gate failed; preserved.
- `a01344bc8`: cold topology directories preserve the120-byte referenceStore;
  collector callbacks run outside retirement gates, with panic-safe cleanup.
- `85e1d8496`: mandatory live-authority checks immediately before callback
  collection and actual nursery-region assertions in every new fixture.
- `c35acfe97`: receipt lifetime registration does not admit sharing a private
  type universe. Preferred shared-type imports remain rejected.
- `79db49daa`: registered receipt claims remain enumerable through the final
  deferred journal interval, after opaque directory removal. Claim decrement
  and final unlink are atomic under the topology writer; heap Close follows unlock.

No native collector, collector configuration or backend production file changed.
The prior root admission, explicit conditional collector slots, transaction
validation,631cb store-retirement proof and10c2006fb finalizer journal are retained.

## Working ownership contract

Every physical table allocation has one canonical container; aliases share it.
Every occupied converted slot owns a distinct checked root and collector claim.
Identity includes the actual opaque conversion, physical container/index and
exact collector. No opaque word is interpreted as compact, and one entry root
cannot impersonate multiple independently reachable owners.

Publisher Close clears its transient conversion state without retaining the
publisher for receipt lifetime. Registered claims keep the collector alive.
The scheduler enumerates those claims and uses a store-level joint entrypoint,
including when no collector-owning Instance remains. It preserves ordered
invocation leases, registration revalidation and coalesced contention retries.

Ordinary and active-callback collections keep conditional roots strong. At full
store quiescence H6 traces the object/producer/provider fixed point. Dead receipts
release through a post-gate journal; callbacks execute after collection locks
are released. No permanent publisher or store root is substituted for ownership.

See [API.md](API.md) for prepare/commit/rollback/release contracts, lease order,
physical alias canonicalization and the parent handoff.

## Deterministic coverage

The new tests invoke real `extern.convert_any` Wasm and the production receipt API
or mirrored-table transaction on real exported provider descriptors. They cover:

- Publisher physical Close with a surviving collector-free provider; two aliases
  with independent roots; receipt-only full collection and final domain release
  without another guest invocation.
- Two independently reachable containers, canonical aliases, equal checked-slot
  indexes in different collectors, same-value replacement, rejected publication,
  rollback and exactly-once release/clear.
- Provider-first, publisher-first and store-first retirement.
- Converted aggregate -> producer -> provider cycles, surviving an external
  edge and reclaiming after its removal; default and actual region shift6.
- Real active host callbacks with pre-collection ownership guards, then eventual
  reclamation; physical callback counts and unlocked callback execution.
- Receipt-only native, other-Runtime native, topology, invocation-domain and
  collection-driver contention; wakeups, coalescing and requests after worker exit.
- Private type-admission isolation and direct collection during the final
  registered-but-journaled domain claim interval.

Existing local-table guest tests exercise the newly wired transaction. Existing
H6/631cb/10c2006fb regression tests remain in the focused and race selections.
Every failed ownership assertion stops before further heap access or collection.

## Qualification

Final pin79db49daa: **18/18 integrated commands PASS; driver exit0; unchanged
export and unchanged driver.** Actual target execution separately passes all14
commands (six builds, six QEMU executions and two emulator-version checks), with
unchanged driver and checked ELF/in-guest architecture.

| Suite on final pin | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused H2/H6/conversion (top-level passed; zero skips) | 37 | 37 | 37 |
| Scoped race, same selection (passed; zero skips) | 37 | 37 | 37 |
| Full Linux AMD64 `./src/wago` (passed / recorded skip entries) | 2492 / 44 | 2486 / 50 | 2492 / 44 |
| Full AMD64/ARM64 backend + Wasm compiler packages (passed / skip entries) | 998 / 552 | 996 / 554 | 1121 / 553 |
| Linux ARM64 cross-build, compile-only | PASS | PASS | PASS |
| Linux RV64 cross-build, compile-only | PASS | PASS | PASS |
| Actual QEMU10.0.13 ARM64 host path (passed; zero skips) | 29 | 29 | 29 |
| Actual QEMU10.0.13 RV64 regression controls (passed; zero skips) | 45 | 45 | 45 |

Focused/race selector: `^Test(R9(H2|Ownership)|GCExternConversion)`. Race is scoped,
not full-package race. Backend packages are `railshot/amd64`, `railshot/arm64`
and `compiler/wasm`. Full-suite/backend skips include existing optional,
architecture and diagnostic cases; exact names remain in each ledger and raw log.
All counts above are top-level passes; skip-entry counts include nested skips.

The authoritative final-pin directories are `integrated-receipt-final/` and
`targets-receipt-final/`. Both reuse the unchanged committed drivers from
`r9-h2-production/`, with separate immutable exports, exact source pins, explicit
Go1.27.3/Node26.11.1/WABT1.0.41/spec-interpreter paths and pinned spec submodules.
Per-command arguments, raw logs, skips, provenance and driver integrity are
retained. `summary.json` and `SHA256SUMS` cover all attempts.

Earlier directories apply only to their own immutable pins. In particular
`integrated-ae64b76c1/` remains FAIL (three unchanged footprint tests), even though
its target run passed. `DEVELOPMENT.md` records the correction and fixture limits.
No partial/failed attempt is relabeled a final pass.

RV64 execution covers existing compact-extern and managed-function regression
controls; it does not claim host opaque receipt support. TinyGo/other-host receipt
support, full native-collector/platform/conformance integration and performance
qualification remain outside this slice. Existing optional and architecture
skips are preserved verbatim, not converted into passes. Incidental Go test
durations are not performance measurements.

## Remaining hooks and sibling crosscheck

Not yet covered: public/backend-inline extern table set/clear; imported table
stores; global/imported-global cells; table fill/copy/init/grow/initialization;
and public disposal or storage relocation adapters for those paths. Bulk hooks
must acquire the full destination range before the first visible write. The
present physical owner teardown consumes core receipts, but does not install
missing publication hooks. No end-to-end support for those paths is claimed.

Read-only sibling audit preceded implementation: Slipstream
`084be3d4bf70fb5337085ceddfab1bfa9db8b1c9`, `src/jit.goose`, registers imported
physical regions and runtime authority before publication. Starshine
`1d6c0af47140299d8f3c01ed03060d279c43ee03`,
`docs/wiki/wasm-gc-core-boundary.md`, describes model/codec/validation boundaries,
with no equivalent native receipt contract. No new sibling defect is inferred.

Owned paths were committed with --only and documentation force-added. Existing
stage2 scripts, shared port log and dirty submodules/index were preserved. No
subagents, Claude reviews, benchmarks, timing workloads, measurement lease,
PRs, pushes or thread closures. Parent owns the remaining hooks and final H2 gate.
