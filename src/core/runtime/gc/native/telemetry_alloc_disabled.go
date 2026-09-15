//go:build !wago_gcstats

package gc

func (c *Collector) allocThroughput(size uint32, space spaceKind) (handleEntry, error) {
	return c.throughput.alloc(size, space)
}
