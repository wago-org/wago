//go:build wago_gcstats

package gc

func (c *Collector) allocThroughput(size uint32, space spaceKind) (handleEntry, error) {
	before := len(c.throughput.mem)
	e, err := c.throughput.alloc(size, space)
	if c.telemetryEnabled() && len(c.throughput.mem) > before {
		c.cfg.Telemetry.paths.BackingGrowths++
		c.cfg.Telemetry.paths.BackingBytesCopied += uint64(before)
	}
	return e, err
}
