//go:build !wago_profile

package shared

import "fmt"

const scalarSourcesEnabled = false

// The checked wrapper embeds this empty holder; ordinary ScalarState omits it.
type scalarSourceState struct{}

func (*ScalarState) sourceAdd(scalarID, ScalarLocation) {}
func (*ScalarState) sourceInstruction(int)              {}
func (*ScalarState) sourceEnterNode(scalarID) uint32    { return 0 }
func (*ScalarState) sourceRestore(uint32)               {}

// CompileScalarWithSources preserves CompileScalar's physical target contract.
// A nil recorder is equivalent to CompileScalar; recording requires wago_profile.
func (s *ScalarState) CompileScalarWithSources(code []byte, summary ScalarSummary, localWide []bool, nParams int, target ScalarTarget, recorder *ScalarSourceRecorder, function, localDeclBytes uint32) (int, error) {
	if recorder != nil {
		return 0, fmt.Errorf("shared scalar: source recording requires wago_profile")
	}
	return s.CompileScalar(code, summary, localWide, nParams, target)
}
