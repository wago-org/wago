//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// Compiled admission repeats the local-cell source profile before executable
// publication. No local mutable reference cell may reach a public global handle.
func rv64ValidateLocalMutableReferences(c *Compiled) (bool, error) {
	if rv64StateOnlyMutableFuncrefImport(c) {
		return false, nil // The sole mutable cell is imported, not local executable state.
	}
	if rv64MutableFuncrefGlobalGet(c) {
		return true, nil // Exact read-only imported cell reuses the full-width load gate.
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
