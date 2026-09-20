package gc

import "testing"

func TestStructCardPayloadRangeBounds(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentDesc, err := NewStructDesc(1, []StorageKind{StorageRefNull, StorageI32, StorageRefNull, StorageRefNull, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{leaf, parentDesc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	var children [4]Ref
	for i := range children {
		children[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, field := range []uint32{0, 2, 3} {
		if err = c.StructSet(parent, field, RefValue(children[i])); err != nil {
			t.Fatal(err)
		}
	}
	// A numeric field with reference-shaped bits must not enter the marked set.
	if err = c.StructSet(parent, 1, I32Value(int32(children[3]))); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name              string
		start, end, slots uint32
		marked            [4]bool
	}{
		{"first", 0, 0, 1, [4]bool{true, false, false, false}},
		{"gap and numeric", 1, 7, 0, [4]bool{}},
		{"inclusive start", 8, 8, 1, [4]bool{false, true, false, false}},
		{"inclusive end", 8, 12, 2, [4]bool{false, true, true, false}},
		{"exclude below start", 9, 12, 1, [4]bool{false, false, true, false}},
		{"null and high end", 13, ^uint32(0), 1, [4]bool{}},
		{"high start", ^uint32(0), ^uint32(0), 0, [4]bool{}},
		{"full uint32 range", 0, ^uint32(0), 4, [4]bool{true, true, true, false}},
		{"reversed", 12, 8, 0, [4]bool{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c.clearNurseryMarks()
			slots, _ := c.scanObjectPayloadRange(handleOf(parent), test.start, test.end)
			if slots != test.slots {
				t.Fatalf("slots=%d want=%d", slots, test.slots)
			}
			for i, child := range children {
				if c.mark[handleOf(child)] != test.marked[i] {
					t.Fatalf("child %d marked=%v want=%v", i, c.mark[handleOf(child)], test.marked[i])
				}
			}
		})
	}
	root := Root(parent)
	if err = c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}
