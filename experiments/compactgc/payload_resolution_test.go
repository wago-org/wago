package compactgc

import "testing"

func TestPayloadResolutionChecksLayoutAndCapsBorrow(t *testing.T) {
	c, err := NewWithTypes(128, 256, []Descriptor{{Kind: KindStruct, Align: 4, PayloadBytes: 4}, {Kind: KindArray, Align: 2, ElementSize: 2}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.NewStruct(0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Payload(s)
	if err != nil || len(p) != 4 || cap(p) != 4 {
		t.Fatal(p, err)
	}
	p[0] = 77
	p2, err := c.Payload(s)
	if err != nil || p2[0] != 77 {
		t.Fatal(p2, err)
	}
	c.put32(uint32(s), ^uint32(0))
	if _, err = c.Payload(s); err != ErrType {
		t.Fatalf("bad type: %v", err)
	}
	a, err := c.NewArray(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	p, err = c.Payload(a)
	if err != nil || len(p) != 6 || cap(p) != 6 {
		t.Fatal(p, err)
	}
	c.put32(uint32(a)+4, ^uint32(0))
	if _, err = c.Payload(a); err != ErrRef {
		t.Fatalf("bad extent: %v", err)
	}
	for _, r := range []Ref{0, I31New(1), Ref(24)} {
		if _, err = c.Payload(r); err != ErrRef {
			t.Fatalf("invalid ref %v: %v", r, err)
		}
	}
}
