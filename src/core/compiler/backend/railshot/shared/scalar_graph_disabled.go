//go:build !wago_regalloccheck

package shared

const scalarGraphChecks = false

// ScalarState contains the sole semantic state for an admitted function.
// A defined type preserves its public identity and exact ordinary fields.
type ScalarState scalarState
type scalarGraphSnapshot struct{}

// Hooks are package functions so ordinary ScalarState method metadata stays
// unchanged even when the linker erases all guarded calls.

func checkGraphBegin(_ *ScalarState, t ScalarTarget) ScalarTarget { return t }
func checkGraphComplete(_ *ScalarState)                           {}
func checkGraphEnd(_ *ScalarState)                                {}
func checkGraphAdd(*ScalarState, scalarID)                        {}
func checkGraphSeed(*ScalarState, scalarID)                       {}
func checkGraphUse(*ScalarState, scalarID)                        {}
func checkGraphInputs(*ScalarState, scalarID, scalarID)           {}
func checkGraphDefine(*ScalarState, scalarID, uint8)              {}
func checkGraphResult(*ScalarState, scalarID)                     {}
func checkGraphElse(_ *ScalarState)                               {}
func checkGraphRestoreCarrier(*ScalarState, bool, uint8)          {}
func checkGraphRestore(*ScalarState, int) scalarGraphSnapshot {
	return scalarGraphSnapshot{}
}
func checkGraphRestored(*ScalarState, scalarGraphSnapshot) {}
