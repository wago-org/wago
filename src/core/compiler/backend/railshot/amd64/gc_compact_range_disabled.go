//go:build amd64 && !(wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || (wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2)) || wago_gc_compact_range_stride)

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type compactRangeState struct{}

func (*fn) prepareCompactRange(*wasm.Reader, gcLoopResolverPlan) {}
func (*fn) tryCompactRangeLatch(*ctrlFrame, int, int, Cond) bool { return false }
