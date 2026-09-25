package gc

import (
	"fmt"
	"testing"
)

// BenchmarkTinyCycleStartHandleReset isolates idle Step initialization by
// forcing idle between calls. It does not complete a collection each time.
func BenchmarkTinyCycleStartHandleReset(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, handles := range []uint32{256, 4096, 65536} {
		b.Run(fmt.Sprintf("handles=%d", handles), func(b *testing.B) {
			heapBytes := handles * 16
			if heapBytes < 4096 {
				heapBytes = 4096
			}
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: heapBytes, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for i := uint32(0); i < handles; i++ {
				if _, err := c.NewStructDefault(0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ReportMetric(float64(handles), "handles")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Isolate cycle initialization. Abandoning the rootless mark phase is
				// intentional here: completing sweep would destroy the fixed live
				// handle population whose reset cost this benchmark controls.
				c.tinyGC.state = tinyIdle
				if err := c.Step(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTinyUnfinishedRestartStart measures rootless start initialization
// against retained color storage, not a complete recovery pause.
func BenchmarkTinyUnfinishedRestartStart(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, handles := range []uint32{4096, 65536} {
		b.Run(fmt.Sprintf("handles=%d", handles), func(b *testing.B) {
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: handles * 16, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for i := uint32(0); i < handles; i++ {
				if _, err := c.NewStructDefault(0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Measure initialization against retained storage, excluding root
				// tracing and sweep. This is not a complete recovery pause.
				c.tinyGC.state = tinyMark
				c.tinyGC.markEpoch = 63
				if err := c.tinyStartMark(nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(c.tinyGC.color)), "color-bytes")
		})
	}
}

func benchmarkTinyRetainedGraph(b *testing.B, recovery bool, failures int, handles uint32, warm bool) {
	fields := make([]StorageKind, 16)
	for i := range fields {
		fields[i] = StorageRefNull
	}
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	parentType, err := NewStructDesc(1, fields)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: handles * 16, TinyBlockBytes: 16}, []TypeDesc{leaf, parentType})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for i := uint32(0); i < handles; i++ {
		if _, err := c.NewStructDefault(0); err != nil {
			b.Fatal(err)
		}
	}
	if err := c.CollectFull(nil); err != nil {
		b.Fatal(err)
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		b.Fatal(err)
	}
	children := make([]Ref, len(fields))
	for i := range fields {
		child, err := c.NewStructDefault(0)
		if err != nil {
			b.Fatal(err)
		}
		children[i] = child
		if err := c.StructSet(parent, uint32(i), RefValue(child)); err != nil {
			b.Fatal(err)
		}
	}
	live := 0
	for h := 1; h < len(c.handles); h++ {
		if c.handles[h].space == spaceTiny {
			live++
		}
	}
	if live != len(children)+1 || len(c.tinyGC.color) != int(handles)+1 {
		b.Fatalf("high-water setup: %d live objects, %d color bytes; want %d and %d", live, len(c.tinyGC.color), len(children)+1, handles+1)
	}
	root := Root(parent)
	roots := Slots{&root}
	if warm {
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if recovery {
			for j := 0; j < failures; j++ {
				failed := tinySecondWalkFailure{root: root}
				if err := c.CollectFull(&failed); err == nil {
					b.Fatal("root callback unexpectedly completed")
				}
			}
		}
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(c.tinyGC.color)), "color-bytes")
	b.ReportMetric(float64(live), "live-objects")
	if err := c.Verify(roots); err != nil {
		b.Fatal(err)
	}
	for i, child := range children {
		field, err := c.StructGet(parent, uint32(i))
		if err != nil || field.Ref != child || !c.validObjectRef(child) {
			b.Fatalf("retained child %d lost: field %v, error %v, valid %v", i, field, err, c.validObjectRef(child))
		}
	}
}

func BenchmarkTinyRetainedGraph(b *testing.B) {
	for _, handles := range []uint32{4096, 65536} {
		b.Run(fmt.Sprintf("complete/handles=%d", handles), func(b *testing.B) {
			benchmarkTinyRetainedGraph(b, false, 0, handles, true)
		})
	}
}

// BenchmarkTinyRetainedHighWaterRecovery keeps 17 live objects after 65,536
// handles have been published. Failed starts clear the retained color length.
func BenchmarkTinyRetainedHighWaterRecovery(b *testing.B) {
	for _, failures := range []int{1, 5} {
		b.Run(fmt.Sprintf("failed-starts=%d", failures), func(b *testing.B) {
			benchmarkTinyRetainedGraph(b, true, failures, 65536, true)
		})
	}
}

func BenchmarkTinyRetainedGraphFirstUse(b *testing.B) {
	for _, handles := range []uint32{4096, 65536} {
		b.Run(fmt.Sprintf("handles=%d", handles), func(b *testing.B) {
			benchmarkTinyRetainedGraph(b, false, 0, handles, false)
		})
	}
}
