//go:build !wago_gcstats

package wago

import "github.com/wago-org/wago/src/core/runtime/gc/native"

func recordSynchronousGCHelper(*Instance, uint32, []uint64) {}
func setGCHelperStatsTracking(*gc.Collector, bool)          {}
func snapshotGCHelperStats(*gc.Collector) GCHelperStats     { return GCHelperStats{} }
