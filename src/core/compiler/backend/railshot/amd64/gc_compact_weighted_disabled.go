//go:build amd64 && !(wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2)

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type compactWeightedState struct{}

func (*fn) prepareCompactWeighted(*wasm.Reader, gcLoopResolverPlan, bool) {}
func (*fn) tryCompactWeightedLatch(*ctrlFrame, int, int, Cond) bool       { return false }
