package gc

import "testing"

func TestForcePromoteRefreshesNativeView(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{
		NurseryBytes:        16 << 10,
		ThroughputHeapBytes: 64 << 10,
		ThroughputPageBytes: 4096,
	}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	view := c.NativeView()
	for i := 0; i < 2; i++ {
		array, err := c.NewArrayDefault(0, 900)
		if err != nil {
			t.Fatal(err)
		}
		generation := view.RefreshGeneration
		if err := c.ForcePromote(array); err != nil {
			t.Fatal(err)
		}
		want := NativeSpaceView{Base: sliceData(c.throughput.mem), Bytes: uint32(len(c.throughput.mem))}
		if view.Spaces[NativeSpaceOld] != want || view.Spaces[NativeSpaceLarge] != want {
			t.Errorf("promotion %d native spaces = %+v/%+v, want %+v", i, view.Spaces[NativeSpaceOld], view.Spaces[NativeSpaceLarge], want)
		}
		if view.RefreshGeneration <= generation {
			t.Errorf("promotion %d did not refresh native generation", i)
		}
	}
}
