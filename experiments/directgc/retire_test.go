package directgc

import (
	"errors"
	"reflect"
	"testing"
)

func TestRetirementMapMatchesHeaders(t *testing.T) {
	for _, words := range [][]uint32{{0}, {1}, {7}, {31}, {0, 1, 2, 7, 31, 127}} {
		c := heap(t, 8192, 8192)
		for n := 0; n < 7; n++ {
			for _, w := range words {
				obj(t, c, 0, w)
			}
		}
		before := append([]bool(nil), c.starts...)
		want := c.retireNurseryHeaders()
		headerResult := append([]bool(nil), c.starts...)
		copy(c.starts, before)
		got := c.retireNurseryMap()
		if got != want || !reflect.DeepEqual(headerResult, c.starts) {
			t.Fatal("retirement mismatch", got, want)
		}
	}
}
func TestRetirementCollectionAccounting(t *testing.T) {
	for _, live := range []bool{false, true} {
		c := heap(t, 8192, 8192)
		roots := []*Ref{}
		values := []Ref{}
		for _, w := range []uint32{0, 1, 2, 7, 31, 127} {
			values = append(values, obj(t, c, 0, w))
		}
		if live {
			for i := range values {
				roots = append(roots, &values[i])
			}
		}
		if e := c.Minor(roots); e != nil {
			t.Fatal(e)
		}
		assertAllocations(t, c, 6)
		if e := c.Full(roots); e != nil {
			t.Fatal(e)
		}
		assertAllocations(t, c, 6)
	}
	c := heap(t, 128, 32)
	a := obj(t, c, 0, 1)
	b := obj(t, c, 0, 1)
	before := append([]bool(nil), c.starts...)
	if e := c.Minor([]*Ref{&a, &b}); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, c.starts) || c.retiredNurseryAllocations != 0 {
		t.Fatal("OOM retired nursery")
	}
	assertAllocations(t, c, 2)
}

// Each iteration includes identical restoration of the start-map input. This
// isolates header-vs-map retirement plus a common copy cost, not complete GC.
func BenchmarkRetirementTraversal(b *testing.B) {
	for _, shape := range []struct {
		name  string
		words []uint32
	}{
		{"tiny16", []uint32{0}}, {"small32", []uint32{1}}, {"large1040", []uint32{128}}, {"mixed", []uint32{0, 1, 7, 31, 128}},
	} {
		b.Run(shape.name, func(b *testing.B) {
			c, e := New(2<<20, 2<<20)
			if e != nil {
				b.Fatal(e)
			}
			for n := 0; ; n++ {
				_, e = c.NewObject(0, shape.words[n%len(shape.words)])
				if errors.Is(e, ErrOOM) {
					break
				}
				if e != nil {
					b.Fatal(e)
				}
			}
			initial := append([]bool(nil), c.starts[c.nurseryStart/8:c.nurseryBump/8]...)
			for _, mapMode := range []bool{false, true} {
				name := "headers"
				if mapMode {
					name = "map"
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(c.nurseryBump - c.nurseryStart))
					var count uint64
					for i := 0; i < b.N; i++ {
						copy(c.starts[c.nurseryStart/8:c.nurseryBump/8], initial)
						if mapMode {
							count = c.retireNurseryMap()
						} else {
							count = c.retireNurseryHeaders()
						}
					}
					if count == 0 {
						b.Fatal("empty benchmark")
					}
				})
			}
		})
	}
}
