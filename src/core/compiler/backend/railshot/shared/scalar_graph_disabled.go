//go:build !wago_regalloccheck

package shared

const scalarGraphChecks = false

// Leading empty state preserves ordinary ScalarState's layout.
//
//lint:ignore U1000 zero-sized counterpart of checked-only graph state
type scalarGraphState struct{}
type scalarGraphSnapshot struct{}

func (*ScalarState) checkGraphBegin(t ScalarTarget) ScalarTarget { return t }
func (*ScalarState) checkGraphComplete()                         {}
func (*ScalarState) checkGraphEnd()                              {}
func (*ScalarState) checkGraphAdd(scalarID)                      {}
func (*ScalarState) checkGraphSeed(scalarID)                     {}
func (*ScalarState) checkGraphUse(scalarID)                      {}
func (*ScalarState) checkGraphInputs(scalarID, scalarID)         {}
func (*ScalarState) checkGraphDefine(scalarID, uint8)            {}
func (*ScalarState) checkGraphResult(scalarID)                   {}
func (*ScalarState) checkGraphElse()                             {}
func (*ScalarState) checkGraphRestoreCarrier(bool, uint8)        {}
func (*ScalarState) checkGraphRestore(int) scalarGraphSnapshot {
	return scalarGraphSnapshot{}
}
func (*ScalarState) checkGraphRestored(scalarGraphSnapshot) {}
