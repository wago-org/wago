//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package gc

import (
	"testing"
	"unsafe"
)

func TestOwnedFixedNurseryLifetime(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewOwnedCollector(Config{NurseryBytes: 4096, SurvivorBytes: 2048, VerifyAfterCollect: true}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.throughput.nurseryMapped || len(c.nursery) != 8192 || uintptr(unsafe.Pointer(unsafe.SliceData(c.nursery)))%16 != 0 {
		t.Fatal("fixed nursery is not an aligned owned mapping")
	}
	for _, b := range c.nursery {
		if b != 0 {
			t.Fatal("new nursery is not zeroed")
		}
	}
	base := unsafe.SliceData(c.nursery)
	object, err := c.NewStruct(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.StructSet(object, 0, I32Value(42)); err != nil {
		t.Fatal(err)
	}
	root := Root(object)
	for range 3 {
		if err = c.CollectMinor(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		got, e := c.StructGet(object, 0)
		if e != nil || got.I32() != 42 {
			t.Fatalf("live value after minor=%v,%v", got, e)
		}
		if unsafe.SliceData(c.nursery) != base {
			t.Fatal("fixed nursery moved")
		}
	}
	c.Close()
	if c.throughput.nurseryMapped || c.nursery != nil || c.NativeView().Spaces[NativeSpaceNursery].Bytes != 0 {
		t.Fatal("completed close retained nursery owner or native view")
	}
	c.Close()
}

func TestOwnedNurseryAdmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owned bool
		cfg   Config
		want  bool
	}{
		{"standalone", false, Config{NurseryBytes: 4096}, false},
		{"late-opt-in", false, Config{NurseryBytes: 4096}, false},
		{"automatic", true, Config{}, false},
		{"default-size-fixed-heap", true, Config{ThroughputHeapBytes: 32 << 20}, false},
		{"default-size-large-object", true, Config{LargeObjectBytes: 16}, false},
		{"default-size-disabled-collection", true, Config{DisableCollection: true}, false},
		{"fixed", true, Config{NurseryBytes: 4096}, true},
		{"tiny", true, Config{Profile: ProfileTiny, TinyHeapBytes: 4096}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c *Collector
			var err error
			if tc.owned {
				c, err = NewOwnedCollector(tc.cfg, nil)
			} else {
				c, err = NewCollector(tc.cfg, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if tc.name == "late-opt-in" {
				c.UseOwnedThroughputBacking()
			}
			if c.throughput.nurseryMapped != tc.want {
				t.Fatalf("mapped=%v want%v", c.throughput.nurseryMapped, tc.want)
			}
		})
	}
}

func TestOwnedNurseryKeepsCollectorSize(t *testing.T) {
	if got := unsafe.Sizeof(Collector{}); got != 3136 {
		t.Fatalf("collector bytes=%d want3136", got)
	}
}
