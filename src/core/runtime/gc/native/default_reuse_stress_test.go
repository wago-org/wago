package gc

import "testing"

func TestRandomizedDefaultObjectReuseStress(t *testing.T) {
	for _, cfg := range []Config{
		{Profile: ProfileTiny, TinyHeapBytes: 4096, PoisonFreed: true},
		{StressNurseryBytes: 256, LargeObjectBytes: 128, ThroughputHeapBytes: 8192, ThroughputPageBytes: 4096, PoisonFreed: true},
	} {
		c := newTestCollector(t, cfg)
		if cfg.Profile == ProfileTiny {
			c.Close()
			c = newTinyTestCollector(t, cfg)
		}
		var seed uint32 = 0x12345678
		for i := 0; i < 100; i++ {
			seed = seed*1664525 + 1013904223
			if seed&1 == 0 {
				arr, err := c.NewArray(2, 1+(seed%16), I32Value(int32(seed)))
				if err != nil {
					t.Fatal(err)
				}
				root := Root(arr)
				if cfg.Profile != ProfileTiny {
					_ = c.CollectFull(Slots{&root})
				}
				root = Root(Null())
				if err := c.CollectFull(Slots{&root}); err != nil {
					t.Fatal(err)
				}
				arr, err = c.NewArrayDefault(2, 1+(seed%16))
				if err != nil {
					t.Fatal(err)
				}
				v, err := c.ArrayGet(arr, 0)
				if err != nil {
					t.Fatal(err)
				}
				if v.I32() != 0 {
					t.Fatalf("numeric default exposed stale value %#x", v.I32())
				}
			} else {
				child, _ := c.NewStructDefault(0)
				obj, _ := c.NewStructDefault(1)
				if err := c.StructSet(obj, 0, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				root := Root(obj)
				if cfg.Profile != ProfileTiny {
					_ = c.CollectFull(Slots{&root})
				}
				root = Root(Null())
				if err := c.CollectFull(Slots{&root}); err != nil {
					t.Fatal(err)
				}
				obj, err := c.NewStructDefault(1)
				if err != nil {
					t.Fatal(err)
				}
				v, err := c.StructGet(obj, 0)
				if err != nil {
					t.Fatal(err)
				}
				if !v.Ref.IsNull() {
					t.Fatalf("ref default exposed stale ref %#x", v.Ref)
				}
			}
		}
	}
}
