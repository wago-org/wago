# Slice B host imports: pending A-first landing

Starting HEAD `67ef1f39e61dd61ee891bdefa5f8218a74aba806`, production
`501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d`. Source is a **pending uncommitted
patch**, not an integrated or qualified implementation. Parent must explicitly
authorize B's commit after A lands. No host acceptance before committed A+B.

The adopted 24-path B allowlist is unchanged. Actual edited/new source paths:

- `src/wago/gc_conversion_store.go`
- `src/wago/gc_conversion_host_import_host_test.go` (new)
- `src/core/compiler/backend/railshot/amd64/extern_host_store_contract_test.go` (new)
- `src/core/compiler/backend/railshot/arm64/extern_host_store_contract_test.go` (new)

All other B production paths preserve existing implementations. A owns adapter
bodies, receipt semantics, host registration and joint reachability. Its additional
`function_environment_linux.go` ownership does not intersect these changes. No
shared port log, support document, historical evidence, combined qualification
output, or A source/test was edited by B. Unrelated worktree/index state is kept.

Single and bulk stores now share physical imported-table resolution. Missing
handle/owner metadata remains rejected; a valid host owner with nil Instance is
passed unchanged to A's adapter. Descriptor length/capacity/extent validation
prevents malformed descriptors from reaching writes or unsigned grow arithmetic.
Imported global resolution retains nil host identity and guards missing cells.
No signature, helper ID 53–58, arity, opaque representation, allocator, or native
reserved-capacity growth policy changed. Public forwarding remains delegated.

Nine new `TestR9H2HostImport...` top-level tests cover real compiled global/table
publication, old collector-free writers, publisher retirement, host collection,
same-value public set, exact clear, independent physical containers, aliases,
both overlap directions with distinguishable source/null snapshots, fill/copy/
init/grow, failed bounds/grow, per-segment initialization trap effects, ordinary
host tokens/null, public global forwarding, type/immutability/foreign-store
rejections, complete two-domain admission, helper rejection without admission,
coalesced native-contention retries, and a compiled converted-function producer cycle with unlocked final reclamation.
Default and actual region-shift-6 cases retain explicit ownership/region guards.
Failed ownership checkpoints stop before subsequent collection, native reads or
cleanup. Failed fixtures remain reachable for diagnosis.

Each backend adds three non-skipping structural contract tests: exact extern-only
routing, frozen helper IDs/arities, and collector-independent hint/loop planning.
They run on either build host; parsing the other target's code is **not** target
execution. Actual native frame correctness is exercised by the guest tests in
AMD64 loop/nested-call fixtures and, after integration, the mandatory ARM64 QEMU lane.

`check-development.py` exports the immutable starting commit and overlays only
the four owned source paths, recording archive and overlay hashes. It compiles
and enumerates nine host tests without executing them, runs the five existing
module-owned hook regressions normal/race, and runs three contract tests in each
backend package, in ordinary/wagodebug/wago_regalloccheck. Each attempt preserves
its own ledger and logs. This is development regression evidence only.

After A authorization: commit only owned source paths with `git commit --only`,
then run every scoped matrix cell (normal/race, exact nonzero census), unchanged
integrated/target/TinyGo drivers against the committed A+B pin, and inspect all
PASS/SKIP counts, ELF/architecture probes and source/driver integrity. No skipped
required case or compile-only lane will be called target execution. Those gates
are pending, as are performance qualification and parent-owned final acceptance.

Read-only sibling crosscheck is recorded in `sibling-readonly.json`. No subagents,
reviews, benchmarks/timings, measurement lease, PR, push, or closure were started.

Development results: all 15 commands passed, with zero skips: each mode ran five
existing hook tests normally and under race, three contract tests per backend,
and a nine-host-test compile census. The final test-only loop/nested-call and
host-contention additions have a separate successful three-mode compile census;
production/backend hashes match the earlier regression overlay. No host tests
were executed. See `development-summary.json` for exact evidence directories.
The final pending patch applies cleanly to its base; its SHA256 and source hashes
are in `pending.json`. Owned exports/caches were removed after preserving logs.
