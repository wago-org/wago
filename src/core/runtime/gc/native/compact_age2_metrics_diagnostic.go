//go:build wago_gc_compact_age2 && wago_gc_compact_age2_metrics && wago_gc_compact_refstruct

package gc

import compactgc "github.com/wago-org/wago/experiments/compactgc"

// CompactAge2Metrics returns untimed diagnostic counters by value only.
func (c *Collector) CompactAge2Metrics() (compactgc.Age2MetricsSnapshot, bool) {
	if c == nil || c.closed || c.direct == nil {
		return compactgc.Age2MetricsSnapshot{}, false
	}
	return c.direct.Age2Metrics()
}
