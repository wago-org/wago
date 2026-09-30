package gc

import "testing"

func newTinyCycleStatsCollector(t testing.TB) *Collector {
	t.Helper()
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestTinyDirectStepsCountCompletedCycles(t *testing.T) {
	requireTinyIncrementalBuild(t)
	c := newTinyCycleStatsCollector(t)
	for cycle := uint64(1); cycle <= 2; cycle++ {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
		for steps := 0; c.tinyGC.state != tinyIdle && steps < 1000; steps++ {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		if c.tinyGC.state != tinyIdle || c.stats.FullCollections != cycle {
			t.Fatalf("cycle %d state/count = %d/%d", cycle, c.tinyGC.state, c.stats.FullCollections)
		}
	}
}

func TestTinyCollectFullCountsOnce(t *testing.T) {
	c := newTinyCycleStatsCollector(t)
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.stats.FullCollections != 1 {
		t.Fatalf("full collections = %d, want 1", c.stats.FullCollections)
	}
}

func BenchmarkTinyCompletedStepCycle(b *testing.B) {
	requireTinyIncrementalBuild(b)
	c := newTinyCycleStatsCollector(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Step(nil); err != nil {
			b.Fatal(err)
		}
		for c.tinyGC.state != tinyIdle {
			if err := c.Step(nil); err != nil {
				b.Fatal(err)
			}
		}
	}
}
