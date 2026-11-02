# DR-1 / DR-2 / DR-3 runtime repairs — local qualification

No inclusion, fresh-review, performance, or final combined-head claim.

Admission distinguishes full managed-function topology from conversion-only
storage admission. Conversion-only invocations freeze topology and lease their
static local/imported domains plus domains with live bound conversion publishers
or outstanding receipt claims (including deferred claim journals). Binding and
retirement change publisher authority under the topology writer; receipt creation
can only use already-bound authority. Helpers never upgrade invocation gates.
Physical owners must already have admission metadata before receipt attachment.

Ordinary replacement bypasses receipt work only when both incoming and outgoing
words authenticate as ordinary tokens or null. Converted clear is not bypassed.
Scalar imports from storage-only producers retain ordinary cross-Runtime behavior;
real managed-function or statically reached conversion authority still requires
the same Runtime. Foreign scalar relays copy only statically reached domains,
not the source Runtime's unrelated conversion domains. Converted publication
without the complete pre-admitted set is rejected before writing. Host callback
suspension uses the actual public invocation root, including a root with no lease.

Non-Linux/TinyGo host-opaque publication is explicitly unsupported and rejected
before helper/public/bulk/active-initialization writes. Ordinary authenticated
externref and null stores remain available, including public Table.SetValue.
Safe conversion itself is not disabled. RV64 retains its separate compact
initialization/global protocol; the new ordinary public table path rejects an
existing converted slot rather than bypassing compact ownership retirement.
Darwin ARM64 executes conversion in the source target model: its new converted
publication tests require actual runner execution. Cross-build is not execution.

The original Claude probes are copied unchanged into durable src/wago tests.
New tests cover ordinary public table semantics, threaded wait/notifier liveness,
publisher-to-receipt-to-retirement admission, and foreign scalar ordinary storage
plus host callback with an unrelated conversion publisher. Existing H2 tests
remain mandatory for conditional cycles, complete domain authority, publication,
retirement, contention, negative ingress, and host/imported containers.

Read-only sibling cross-check used Slipstream 084be3d4bf70fb5337085ceddfab1bfa9db8b1c9
src/jit.goose:291-305 (actual heap/root-kind gated imported registration), and
Starshine 1d6c0af47140299d8f3c01ed03060d279c43ee03
in starshine-master-base, docs/wiki/wasm-gc-core-boundary.md (model/validator,
not a native receipt runtime). No sibling defect asserted.

Development logs are retained verbatim. Development-3 failed to compile a new
test assertion (an any-typed panic helper used as error); corrected in subsequent
attempts. Development runs use the dirty owned source and are not immutable
qualification. No failed attempt is relabeled as a pass.

Ownership: src/wago runtime/test paths and this evidence directory only. No
backend, collector, TinyGo implementation, CI, measurement-doc, shared-log,
submodule, or unrelated index edits. No benchmarks, timings, lease, PR, push,
subagents, or Claude review. Parent owns fresh review and final combined gates.

## Admission proof and scope

A new opaque identity can be created during native execution. Admitting only
currently occupied receipt claims would miss that first identity. Therefore a
bound conversion core contributes publisher authority before native entry, and
its domain remains admitted after publisher Close while any receipt claim or
final deferred claim journal exists. The last publisher decrement happens under
the existing retirement topology writer; the last claim decrement/unlink remains
under the existing journal writer. Neither path installs unconditional roots or
changes the H2 joint conditional reachability algorithm. Storage-only instances
still freeze topology across native execution; arbitrary callbacks and atomic
wait suspend that freeze. This repair narrows collector gates, not the topology
writer protocol. Allocation/sorting of the narrowed domain view is measurement
sensitive and has not been benchmarked.

Native ARM64 global.set and table set/fill/copy/grow/init emit helpers 54 and
53/55/56/57/58 respectively (`arm64/globals.go:186`, `arm64/table.go`). Dispatch is
platform-independent in `src/wago/gc_conversion_store.go`; therefore Darwin
ARM64 reaches the unsupported adapter. Local legacy table transactions retain
their checked-root acquire/publish/release semantics; imported-table transactions
reject opaque publication on targets without receipts. The unsupported adapter
records physical owner classification at bind and skips imported descriptors on
teardown. It does not supply H2 host receipts. RV64 compact ingress/initialization
and global ownership stay on their existing architecture-specific paths.

## Retained attempts

- `6a17679bf145b1966079dac3b8a4e436c4e7db33`: immutable Linux focused and ordinary
  full suite passed before the run was superseded. ARM64 wait/notifier failed
  because QEMU rejected native deadline-timer creation (`invalid argument`).
  No liveness pass is claimed for that attempt. The watchdog test was repaired
  in `c27c1ffd384715412ebc079579e630aadbfbecaf`.
- `c27c1ffd384715412ebc079579e630aadbfbecaf`: ARM64 QEMU passed all three modes;
  RV64 exposed an overly broad rejection in the legacy local-table transaction
  control. Its failed logs remain intact. Nonhost builds passed all six cells,
  but target execution was NOT_RUN. The integrated run was superseded after
  its completed gates; incomplete cells are not counted as passes.
- `265ca3ffe`: preserves safe local-table transactions while retaining imported
  opaque rejection, and adds deterministic concurrent publication/retirement.
- `2a6dd04b10bdbd51acc414311df149d5e08f9db9`: also preserves imported physical
  descriptors on unsupported-target teardown. This pin completed the recorded Linux/target/build matrices; the final
  source pin also tightens converted-token classification as described below.

The concurrent regression holds native access after public publication has
acquired its complete invocation set, waits for physical retirement to queue at
the topology writer, then allows publication. It requires the exact receipt to
survive publisher teardown, clears it, and rejects the retired token. Failed
ownership checkpoints stop before subsequent heap access or collection.

- `9180f09f7590d029bc90df2fa20c0decc590505a`: final source/test pin. Source audit
  identified that RV64 compact converted public tokens occupy the externref
  directory. The ordinary-only unsupported adapter now authenticates the raw
  slot and rejects ownership wrappers before public-value unwrapping. A durable
  negative test exercises this distinction on RV64 and non-Linux builds. No
  ordinary token or existing compact conversion adapter is reclassified.

## Handoff boundaries

Darwin ARM64 and Windows AMD64 execution remain NOT_RUN: no executable runner
was available in this assignment. Their all-three-mode test binaries compile,
including the non-Linux converted-publication rejection tests. This does not
establish a target pass. TinyGo execution is also NOT_RUN. The unsupported
ordinary Table.SetValue adapter rejects converted incoming tokens and existing
converted slots; it does not replace RV64's compact ownership APIs. Existing
safe local mirrored-table conversion transactions remain available.

Parent owns fresh Claude re-review, final combined-head qualification and any
final inclusion decision. No aggregate inclusion claim is made. The nonhost
execution gate remains open, so no unrestricted READY TO MEASURE declaration is
made by this report. No performance result exists for these changes.

When the parent releases the correctness gates, independent measurement should
use branch `experiment/rfwgc-port`, source `9180f09f7590d029bc90df2fa20c0decc590505a`.
Sensitive paths: invocation-domain selection/suspension, extern scalar and bulk
publication, public table/global writes, publisher binding and receipt retirement.
Proposed workloads: ordinary non-GC extern global/table set/fill/copy/init/grow;
unrelated GC tenants with scalar relays; many bound conversion publishers versus
receipt-only domains; converted replacement/clear and final claim retirement;
and cross-Runtime ordinary scalar relays with host callbacks. The measurement
worker must hold the global lease. This worker ran no benchmarks or timings and
acquired no measurement lease.


## Final immutable qualification

Final source/test pin: `9180f09f7590d029bc90df2fa20c0decc590505a`.
All 18 integrated commands, 14 target commands (six builds, six QEMU executions,
two emulator identity checks), and six nonhost build commands passed. The
integrated source export and driver remained unchanged. Required regression
census assertions passed; original Claude probe files are byte-identical to
those in final-review-design (`original-probes.json`).

Counts below are top-level passes / recorded skip entries; nested skip entries
are not claimed as top-level tests. Full runtime/backend skips retain existing
optional, diagnostic and architecture exclusions verbatim in raw logs.

| Suite | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Linux AMD64 focused | 64 / 0 | 64 / 0 | 64 / 0 |
| Linux AMD64 full src/wago | 2520 / 44 | 2514 / 50 | 2520 / 44 |
| Linux AMD64 scoped race | 64 / 0 | 64 / 0 | 64 / 0 |
| Full AMD64/ARM64 backend + Wasm compiler | 1006 / 552 | 1004 / 554 | 1129 / 553 |
| Linux ARM64 QEMU actual execution | 56 / 0 | 56 / 0 | 56 / 0 |
| Linux RV64 QEMU actual execution | 47 / 0 | 47 / 0 | 47 / 0 |
| Darwin ARM64 build only | PASS | PASS | PASS |
| Windows AMD64 build only | PASS | PASS | PASS |

Race is scoped to the focused selection, not full-package race. ARM64 runs
repair/H2/conversion tests; RV64 runs the ordinary public table and wrapper guard
plus existing compact/managed-function controls, not host-opaque H2 receipts.
The QEMU driver verifies ELF machine and in-guest GOOS/GOARCH. Its inherited
test-only architecture probe and legacy portability fixture adapter are recorded
with hashes in source.json; production sources are the immutable pin.

`summary.json`, per-command JSON/raw logs, source/driver/ELF/binary hashes and
`SHA256SUMS` are the evidence index. Earlier successful or failed attempts apply
only to their named pins. No nonhost execution, TinyGo execution, full conformance
campaign, performance result, fresh reviewer approval or final inclusion is
inferred from these results. Parent must execute the nonhost regressions and
rerun its combined-head gate. All source edits are committed; runtime ownership
can return to the integration owner for fresh review and subsequent repairs.
