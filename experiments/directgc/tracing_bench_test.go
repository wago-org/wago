package directgc

import "testing"

// Repeated full collections retain the same graph. Construction is outside the
// timed region; the root is rewritten after each move. This is a collector-only
// diagnostic, not an end-to-end Wasm or V8 benchmark.
func BenchmarkFullTracingShapes(b *testing.B) {
	for _, shape := range []struct {
		name  string
		desc  Descriptor
		dense bool
	}{
		{"scalarStruct", Descriptor{Kind: KindStruct, Align: 8, PayloadBytes: 8}, false},
		{"scalarArray", Descriptor{Kind: KindArray, Align: 8, ElementSize: 4}, false},
		{"refStruct", Descriptor{Kind: KindStruct, Align: 8, PayloadBytes: 8, RefOffsets: []uint32{0}}, false},
		{"refArray", Descriptor{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, false},
		{"denseRefArray", Descriptor{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, true},
	} {
		b.Run(shape.name, func(b *testing.B) {
			c, err := NewWithTypes(256<<10, 256<<10, []Descriptor{
				{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, shape.desc,
			})
			if err != nil {
				b.Fatal(err)
			}
			root, err := c.NewArray(0, 256)
			if err != nil {
				b.Fatal(err)
			}
			for i := uint32(0); i < 256; i++ {
				var child Ref
				if shape.desc.Kind == KindArray {
					child, err = c.NewArray(1, 32)
				} else {
					child, err = c.NewStruct(1)
				}
				if err != nil {
					b.Fatal(err)
				}
				if err := c.SetRef(root, i, child); err != nil {
					b.Fatal(err)
				}
				if shape.desc.ElementRef || len(shape.desc.RefOffsets) != 0 {
					slots := uint32(1)
					if shape.dense {
						slots = 32
					}
					for j := uint32(0); j < slots; j++ {
						if err := c.SetRef(child, j, root); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
			roots := []*Ref{&root}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.Full(roots); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
