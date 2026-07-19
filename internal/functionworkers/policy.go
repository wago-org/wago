// Package functionworkers resolves Wago's bounded per-module function worker
// policy for validation and native code generation.
package functionworkers

import "runtime"

const (
	perFunctionWeight = 64
	parallelThreshold = 16 << 10
	autoWorkerLimit   = 4
)

// Resolve converts a configured policy into an effective worker count. Zero is
// adaptive, one is serial, and values above one are forced maxima. The result is
// always between one and the local-function count, capped by GOMAXPROCS.
func Resolve(policy, functions, bodyBytes int) int {
	if functions <= 1 {
		return 1
	}
	if bodyBytes < 0 {
		bodyBytes = parallelThreshold
	}
	workers := policy
	if workers == 0 {
		// Avoid an overflowing score for defensive/programmatic module shapes.
		// For decoded modules bodyBytes is bounded by the source allocation.
		if bodyBytes < parallelThreshold {
			remaining := parallelThreshold - bodyBytes
			neededFunctions := (remaining + perFunctionWeight - 1) / perFunctionWeight
			if functions < neededFunctions {
				return 1
			}
		}
		workers = autoWorkerLimit
	}
	if workers < 1 {
		return 1
	}
	if max := runtime.GOMAXPROCS(0); workers > max {
		workers = max
	}
	if workers > functions {
		workers = functions
	}
	if workers <= 1 {
		return 1
	}
	return workers
}
