package gc

import "testing"

func TestArrayInitializerRootScratchPreservesReferencesAcrossCollection(t *testing.T) {
	profiles := []struct {
		name string
		cfg  Config
	}{
		{name: "throughput", cfg: Config{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}},
		{name: "tiny", cfg: Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			obj, _ := NewStructDesc(0, nil)
			refs, _ := NewArrayDesc(1, StorageRef)
			c, err := NewCollector(tc.cfg, []TypeDesc{obj, refs})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			child, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			var scratch ArrayInitializerRootScratch
			uniform, err := c.NewArrayWithRootScratch(1, 4, RefValue(child), EmptyRoots{}, &scratch)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.ArrayGet(uniform, 3)
			if err != nil || got.Ref != child {
				t.Fatalf("uniform reference = %+v, %v; want %v", got, err, child)
			}

			values := []Value{RefValue(child), RefValue(child)}
			fixed, err := c.NewArrayFixedWithRootScratch(1, values, EmptyRoots{}, &scratch)
			if err != nil {
				t.Fatal(err)
			}
			got, err = c.ArrayGet(fixed, 1)
			if err != nil || got.Ref != child {
				t.Fatalf("fixed reference = %+v, %v; want %v", got, err, child)
			}
			if typ, err := c.ObjectType(got.Ref); err != nil || typ != 0 {
				t.Fatalf("preserved child type = %d, %v; want 0", typ, err)
			}
		})
	}
}
