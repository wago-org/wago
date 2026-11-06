//go:build tinygo || wago_target_tinygo

package gc

const parallelCollectionSupported = false

type parallelCollector struct{}

func (*Collector) drainParallel(bool, bool) (uint64, bool) { return 0, false }
func (*Collector) closeParallel()                          {}

func (*Collector) sweepParallel() bool                           { return false }
func (*Collector) copyPromotionParallel([]plannedPromotion) bool { return false }
