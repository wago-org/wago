//go:build !wago_gc_compact_age2 || !wago_gc_compact_age2_metrics

package compactgc

const age2MetricsEnabled = false

type age2MetricsState struct{}
type age2CycleMetric struct{}

func (c *Collector) beginAge2Metric(bool) age2CycleMetric            { return age2CycleMetric{} }
func (c *Collector) captureAge2Metric(*age2CycleMetric, int, uint32) {}
func (c *Collector) finishAge2Metric(age2CycleMetric, error)         {}
func (d *age2CycleMetric) rememberedParent()                         {}
func (d *age2CycleMetric) rememberedEdge()                           {}
