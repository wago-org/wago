//go:build arm64

package arm64

func (c *sharedTrapBodyCluster) reset() { c.n = 0 }
