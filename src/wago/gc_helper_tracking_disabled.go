//go:build !wago_gcstats

package wago

import "github.com/wago-org/wago/src/core/runtime/gc"

func recordSynchronousGCHelper(*gc.Collector, uint32)   {}
func setGCHelperStatsTracking(*gc.Collector, bool)      {}
func snapshotGCHelperStats(*gc.Collector) GCHelperStats { return GCHelperStats{} }
