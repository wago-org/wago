package gc

import "testing"

// Independent review probe: a full sweep must not count survivor-space handles
// as Eden occupants or reset storage that still contains a live Eden payload.
func TestStage3ReviewSurvivorAndEdenReuse(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 128, NurseryRegionShift: 6, SurvivorBytes: 128, PoisonFreed: true, VerifyAfterCollect: true}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	survivor := regionStruct(t, c, 0, EmptyRoots{})
	slot := c.NewGlobalSlot(survivor)
	if err := c.StructSet(survivor, 1, I64Value(123)); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	before := *c.entry(survivor)
	if before.off < c.edenBytes() {
		t.Fatal("probe failed to enter survivor space")
	}
	for i := 0; i < 3; i++ {
		regionStruct(t, c, 0, EmptyRoots{})
	}
	eden := regionStruct(t, c, 0, EmptyRoots{})
	root := Slots{(*Root)(&eden)}
	if err := c.StructSet(eden, 1, I64Value(456)); err != nil {
		t.Fatal(err)
	}
	edenBefore := *c.entry(eden)
	for i := 0; i < 64; i++ {
		regionStruct(t, c, 0, root)
		if err := c.CollectFull(root); err != nil {
			t.Fatal(err)
		}
		if *c.entry(survivor) != before || *c.entry(eden) != edenBefore {
			t.Fatal("full sweep moved retained payload")
		}
		for _, check := range []struct {
			ref   Ref
			value int64
		}{{survivor, 123}, {eden, 456}} {
			v, err := c.StructGet(check.ref, 1)
			if err != nil || v.I64() != check.value {
				t.Fatalf("payload %v: %v %v", check.ref, v, err)
			}
		}
	}
	if err := c.CollectMinor(root); err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(slot, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatal("dropped graph retained")
	}
}

func TestStage3ReviewConfigShiftDomain(t *testing.T) {
	for shift := 0; shift < 256; shift++ {
		err := ValidateConfig(Config{NurseryBytes: 128, NurseryRegionShift: uint8(shift)})
		valid := shift == 0 || shift == 6 || shift == 7
		if (err == nil) != valid {
			t.Fatalf("shift %d: %v", shift, err)
		}
	}
}
