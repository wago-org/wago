# Preserved development and qualification limits

The first immutable integrated attempt, `integrated-ae64b76c1`, is FAIL.
Focused tests passed; full ordinary Wago failed exactly three footprint checks:
`TestFuncrefReferenceStoreStructFootprint`,
`TestHostCreatedFuncRefGlobalPersistenceAndLayoutsStayFailClosed`, and
`TestReferenceGlobalPersistenceAndFootprintsStayBounded`.
The referenceStore had grown from120 to136 bytes. Sourcea01344bc8 moves the
receipt directories into the existing cold topology sidecar without changing
those test expectations. The original driver stopped at command2, verified an
unchanged export and driver, and exited1. Missing commands are not passes.
The independent ae64 target run passed but does not waive this full-suite failure.

Earlier uncommitted development fixture corrections:

- Registering private conversion domains initially enabled shared-frame walking
  for private frame probes. Preserve the private type/frame universe while
  registering independent lifetime authority. The existing forged-frame test
  remains unchanged and passes.
- A cycle fixture assumed an imported function's descriptor belonged to its
  original producer; it actually retained an importer proxy. Its ownership
  assertion stopped before further collection or access.
- An imported-table cycle variant had no collecting root-map admission; the
  default variant stopped at the absent producer-edge assertion and the region
  variant rejected configuration before publication. No disabled-collection
  bypass was used. The final fixture uses the already-admitted imported extern
  global dependency and installs its conversion into the real provider table
  through the receipt transaction. Public/global write hooks are not claimed.

The final test-only followup85e1d8496 strengthens pre-collection invariants and
asserts actual region policy in every new fixture. Subsequent production corrections retain shared-type admission isolation
(c35acfe97) and enumerate final deferred claims independently of opaque directory
rows (79db49daa). The `receipt-final` directories are authoritative for the final
79db49daa source/test pin. Earlier `final`, `verified`, and `qualification`
directories apply only to their own recorded immutable pins; they never substitute
for the final pin. The deferred-claim regression asserts registered authority
before directly invoking joint collection, so a missing authority stops safely.

No native collector/configuration/backend production file, shared port log,
stage2 script, dirty submodule or unrelated index entry was edited. No subagent,
review service, benchmark/timing workload, lease, PR, push or thread closure.
