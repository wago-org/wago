package gc

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func rfw3Collector(t testing.TB, on bool) *Collector {
	cfg := Config{NurseryBytes: 4096, SurvivorBytes: 4096}
	// Reflection keeps the identical harness compilable at the pre-region pin.
	if on {
		field := reflect.ValueOf(&cfg).Elem().FieldByName("NurseryRegionShift")
		if !field.IsValid() {
			t.Fatal("region On unsupported at this revision")
		}
		field.SetUint(8)
	}
	c, err := NewCollector(cfg, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func rfw3Cycle(t testing.TB, c *Collector, name string) {
	var root Root
	roots := Slots{&root}
	check := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 256
	if name == "graph" {
		count = 64
	}
	for i := 0; i < count; i++ {
		typ := TypeID(0)
		if name == "graph" || name == "high" {
			typ = 1
		}
		var r Ref
		var err error
		if name == "large" {
			r, err = c.NewArrayDefaultWithRoots(2, 16384, roots)
		} else {
			r, err = c.NewStructDefaultWithRoots(typ, roots)
		}
		check(err)
		if typ == 1 {
			check(c.StructSet(r, 0, RefValue(Ref(root))))
		}
		if name == "high" || name == "graph" || name == "promotion" || (name == "low" && i == 0) {
			root = Root(r)
		}
		if name == "promotion" {
			check(c.CollectMinor(roots))
			check(c.CollectMinor(roots))
			if c.entry(r).space != spaceOld {
				t.Fatal("promotion did not reach old space")
			}
		}
	}
	check(c.CollectFull(roots))
	want := uint32(0)
	switch name {
	case "high", "graph":
		want = uint32(count)
	case "promotion", "low":
		want = 1
	}
	if c.Stats().LiveObjects != want {
		t.Fatalf("%s live=%d want=%d", name, c.Stats().LiveObjects, want)
	}
	check(c.Verify(roots))
	if test, ok := t.(*testing.T); ok {
		raw, _ := json.Marshal(rfw3Memory(c))
		test.Log("live checkpoint " + string(raw))
	}
	check(c.CollectFull(EmptyRoots{}))
	if c.Stats().LiveObjects != 0 {
		t.Fatal("cleanup retained objects")
	}
}

func TestRFWStage3MeasurementNative(t *testing.T) {
	modes := []bool{false}
	if reflect.TypeOf(Config{}).NumField() > 0 {
		if _, ok := reflect.TypeOf(Config{}).FieldByName("NurseryRegionShift"); ok {
			modes = append(modes, true)
		}
	}
	for _, on := range modes {
		for _, name := range []string{"scalar", "graph", "low", "high", "promotion", "large"} {
			t.Run(map[bool]string{false: "Off", true: "On"}[on]+"/"+name, func(t *testing.T) {
				c := rfw3Collector(t, on)
				rfw3Cycle(t, c, name)
				want := uint64(256)
				if name == "graph" {
					want = 64
				}
				if c.Stats().Allocations != want {
					t.Fatal(c.Stats())
				}
				if name == "promotion" && c.Stats().PromotedBytes == 0 {
					t.Fatal("no promotion")
				}
				data, _ := json.Marshal(c.Stats())
				t.Log(string(data))
			})
		}
	}
}

func BenchmarkRFWStage3Native(b *testing.B) {
	for _, name := range []string{"scalar", "graph", "low", "high", "promotion", "large"} {
		b.Run(name, func(b *testing.B) {
			c := rfw3Collector(b, os.Getenv("RFW_REGION") == "On")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rfw3Cycle(b, c, name)
			}
			b.StopTimer()
			s := c.Stats()
			b.ReportMetric(float64(s.Allocations)/float64(b.N), "charges/op")
			b.ReportMetric(float64(s.FullCollections)/float64(b.N), "full/op")
			b.ReportMetric(float64(s.MinorCollections)/float64(b.N), "minor/op")
			b.ReportMetric(float64(s.YoungBytesCopied)/float64(b.N), "copied-B/op")
			b.ReportMetric(float64(s.PromotedBytes)/float64(b.N), "promoted-B/op")
			data, _ := json.Marshal(rfw3Memory(c))
			b.Log(string(data))
		})
	}
}

// Physical layout bytes and retained backing are distinct from allocation charges.
// This diagnostic scan is outside benchmark timing and supports both pinned layouts.
func rfw3Memory(c *Collector) map[string]any {
	spaces := map[uint8]uint64{}
	for _, e := range c.handles {
		if e.space != spaceFree {
			spaces[uint8(e.space)] += uint64(e.size)
		}
	}
	regionUsed := []uint64{}
	field := reflect.ValueOf(c).Elem().FieldByName("edenRegions")
	if field.IsValid() {
		for i := 0; i < field.Len(); i++ {
			regionUsed = append(regionUsed, field.Index(i).FieldByName("bump").Uint())
		}
	}
	return map[string]any{"stats": c.Stats(), "layout_bytes_by_space": spaces,
		"region_used_extents": regionUsed, "eden_bump": c.nurseryBump,
		"survivor_bump": c.survivorBump, "nursery_backing_len": len(c.nursery),
		"nursery_backing_cap": cap(c.nursery), "throughput_backing_len": len(c.throughput.mem),
		"throughput_backing_cap": cap(c.throughput.mem), "handles_len": len(c.handles),
		"handles_cap": cap(c.handles)}
}
