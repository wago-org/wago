//go:build amd64 && !wago_experimental_scalar_loop_entry

package amd64

type scalarLoopAlignmentBudget struct{}

func (f *fn) alignExperimentalScalarLoop(int, int) bool { return false }
