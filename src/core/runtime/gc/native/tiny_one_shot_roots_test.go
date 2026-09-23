package gc

import "testing"

type oneShotDirectRoots struct {
	ref   Ref
	calls int
}

func (r *oneShotDirectRoots) RangeRoots(func(RootSlot) bool) {}

func (r *oneShotDirectRoots) RangeRootRefs(sink RootRefSink) bool {
	r.calls++
	if r.calls == 1 {
		return sink.VisitRootRef(r.ref)
	}
	return true
}

func TestTinyKeepsOneShotDirectRoot(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		profile Profile
	}{{"tiny", ProfileTiny}, {"throughput", ProfileThroughput}} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCollector(Config{Profile: tc.profile, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			object, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			roots := &oneShotDirectRoots{ref: object}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if !c.validObjectRef(object) {
				t.Fatalf("profile %d freed one-shot root after %d walks", tc.profile, roots.calls)
			}
		})
	}
}

func BenchmarkTinyFullWithDirectRoot(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	object, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	root := Root(object)
	roots := Slots{&root}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
	}
}
