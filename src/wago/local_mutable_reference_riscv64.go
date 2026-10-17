//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// Compiled admission repeats the local-cell source profile before executable
// publication. No local mutable reference cell may reach a public global handle.
func rv64ValidateLocalMutableReferences(c *Compiled) (bool, error) {
	if rv64PrimitiveReferenceContract(c) {
		return true, nil
	}
	if rv64StateOnlyMutableFuncrefImport(c) {
		return false, nil // The sole mutable cell is imported, not local executable state.
	}
	if rv64MutableFuncrefGlobalGet(c) {
		return true, nil // Exact read-only imported cell reuses the full-width load gate.
	}
	if rv64MutableFuncrefGlobalClear(c) {
		return true, nil // Exact imported clear mutates only through the parked owner bridge.
	}
	if rv64MutableFuncrefGlobalStoreSelf(c) {
		return true, nil // Exact self-store transfers one authenticated descriptor root.
	}
	if rv64MutableFuncrefGlobalCopy(c) {
		return true, nil // Exact cross-container copy transfers the canonical descriptor root.
	}
	if rv64MutableFuncrefGlobalMove(c) {
		return true, nil // Exact move transfers the owner root before clearing its source.
	}
	if rv64MutableFuncrefGlobalBidirectionalCopy(c) {
		return true, nil // Exact indexed copies mutate either imported container safely.
	}
	if rv64MutableFuncrefGlobalLocalCopy(c) {
		return true, nil // Exact mixed-state copy publishes one own local descriptor.
	}
	if rv64MutableFuncrefGlobalLocalPublish(c) {
		return true, nil // Exact mutable private state publishes only null or own function zero.
	}
	if rv64MutableFuncrefGlobalLocalTransfer(c) {
		return true, nil // Exact private container owns captured foreign descriptors.
	}
	if rv64MutableFuncrefGlobalLocalExport(c) {
		return true, nil // Exact exported container routes every guest write through ownership.
	}
	if rv64MutableFuncrefGlobalLocalObserve(c) {
		return true, nil // Exact exported container adds only a nullness observer.
	}
	if rv64MutableFuncrefGlobalLocalCall(c) {
		return true, nil // Exact exported typed container has one owner-scoped call_ref consumer.
	}
	has := false
	for _, g := range c.Globals {
		has = has || g.Type == ValFuncRef && g.Mutable
	}
	if !has {
		return false, nil
	}
	if c.tableCount() != 0 {
		if !c.nativeReferenceFunctionABI {
			return false, fmt.Errorf("local mutable reference cells require no tables")
		}
		// This full shape proof does not call the mutable-cell helper. Permission
		// alone cannot open a host/foreign table ingress or private target row.
		if err := rv64ValidateReferenceFunctions(c); err != nil {
			return false, err
		}
	}
	for _, g := range c.GlobalImports {
		if isReferenceValType(g.Type) {
			return false, fmt.Errorf("local mutable reference cells exclude reference imports")
		}
	}
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) {
			return false, fmt.Errorf("local mutable reference export index")
		}
		g := c.Globals[index]
		if g.Type == ValFuncRef && g.Mutable {
			return false, fmt.Errorf("mutable reference global export is pending")
		}
	}
	// The enclosing validator independently checks every exact global/type and
	// actual function binding. Reference-bearing declarations describe internal
	// controls only; callable signatures remain numeric/vector-only.
	return true, nil
}
