package gc

import "testing"

func TestCheckedHandleGenerationRetiresBeforeWrap(t *testing.T) {
	generations := []uint64{0, ^uint64(0)}
	c := Collector{handles: make([]handleEntry, 2), freeHandles: []uint32{1}, checkedHandles: &generations}
	h := c.newHandle(handleEntry{size: 16})
	if h != 2 || generations[1] != ^uint64(0) || generations[2] != 1 {
		t.Fatalf("handle=%d generations=%v", h, generations)
	}
}
