//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// Compiled admission repeats the source profile before executable publication.
// No mutable reference cell may be reachable through a public global handle.
func rv64ValidateLocalMutableReferences(c *Compiled) (bool, error) {
	has := false
	for _, g := range c.Globals {
		has = has || g.Type == ValFuncRef && g.Mutable
	}
	if !has {
		return false, nil
	}
	if c.tableCount() != 0 {
		return false, fmt.Errorf("local mutable reference cells require no tables")
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
