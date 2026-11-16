//go:build amd64 && !wago_gc_compact_range_loop

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type compactRangeState struct{}

func (*fn) prepareCompactRange(*wasm.Reader, gcLoopResolverPlan) {}
func (*fn) tryCompactRangeLatch(*ctrlFrame, int, int, Cond) bool { return false }
