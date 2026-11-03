//go:build tinygo || wago_target_tinygo

package gc

const parallelCollectionSupported = false

type parallelCollector struct{}

func (*Collector) drainParallel(bool, bool) (uint64, bool) { return 0, false }
func (*Collector) closeParallel()                          {}

func (*Collector) sweepParallel() bool                           { return false }
func (*Collector) copyPromotionParallel([]plannedPromotion) bool { return false }

func (*Collector) parallelError() error           { return nil }
func (*Collector) beginParallelCollection() error { return nil }

func (*Collector) parallelPoisonError() error { return nil }

// The coordinator executes the serial copy loop on TinyGo. Record the actual
// participant just as the standard-Go serial fallback does.
func (c *Collector) copyMatureParallel([]plannedPromotion) bool {
	c.throughput.mature.diagnostics.CopyWorkers = 1
	return false
}

func (*Collector) parallelActivity() ParallelActivity { return ParallelActivity{} }
