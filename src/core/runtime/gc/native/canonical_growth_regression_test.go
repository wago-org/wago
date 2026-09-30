package gc

import "testing"

func TestReviewCanonicalAfterTypeGrowth(t *testing.T) {
	d0, _ := NewStructDesc(0, nil)
	c, err := NewCollector(Config{}, []TypeDesc{d0})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	canonical, err := c.NewTypeCanonicalization([]TypeID{0})
	if err != nil {
		t.Fatal(err)
	}
	d1, _ := NewStructDesc(1, nil)
	if err := c.AddTypes([]TypeDesc{d1}); err != nil {
		t.Fatal(err)
	}
	r, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []bool{false, true} {
		target := RefTestTarget{Kind: RefTestDefined, Type: 0, Exact: exact}
		if matched, err := c.RefTestCanonical(r, target, canonical); err == nil || matched {
			t.Fatalf("stale test = %v, %v", matched, err)
		}
		if _, err := c.RefCastCanonical(r, target, canonical); err == nil {
			t.Fatal("stale cast succeeded")
		}
	}
	fresh, err := c.NewTypeCanonicalization([]TypeID{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := c.RefTestCanonical(r, RefTestTarget{Kind: RefTestDefined, Type: 1}, fresh); err != nil || !matched {
		t.Fatalf("fresh test = %v, %v", matched, err)
	}
}
