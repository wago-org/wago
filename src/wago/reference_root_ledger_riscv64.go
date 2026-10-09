//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

// The shadow phase classifies every physical resource root as anonymous external
// reachability. It neither collects cycles nor changes lifetime decisions.
// lifeMu alone protects these counters; this phase does not establish the lock
// order required by a future dependency graph or component collector.
type referenceRootLedger struct {
	external int
}

// Keep Instance unchanged and allocate the node only on successful acquisition.
// Reuse its zero-count state so warmed scope/root traffic allocates nothing.
type referenceRootAccounting struct {
	ledger       *referenceRootLedger             // protected by the owning Instance's lifeMu
	dependencies *rv64FunctionDependencyOwnership // cold native-function attachment records
}

// Called after resourceRefs increments, with lifeMu held.
func railshotResourceRootAcquiredLocked(in *Instance) {
	accounting := &in.ensureImportState().rootAccounting
	if accounting.ledger == nil {
		// Any preexisting roots are conservatively external as well. No source or
		// descriptor provenance is inferred from this accounting-only snapshot.
		accounting.ledger = &referenceRootLedger{external: in.resourceRefs}
		return
	}
	accounting.ledger.external++
}

// Called after a successful resourceRefs decrement, with lifeMu held. A missing
// node is possible for synthetic/preexisting roots; release stays allocation-free.
func railshotResourceRootReleasedLocked(in *Instance) {
	if state := in.importState.Load(); state != nil && state.rootAccounting.ledger != nil {
		state.rootAccounting.ledger.external--
	}
}
