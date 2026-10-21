package wasm

import (
	"fmt"
	"testing"
)

// BenchmarkSupertypeChainQueries measures the repeated ancestry checks made
// while validating references to one supertype in a long type chain.
func BenchmarkSupertypeChainQueries(b *testing.B) {
	for _, count := range []int{125, 250, 500, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := &Module{Types: make([]RecType, count)}
			for i := range m.Types {
				var supers []TypeIdx
				if i != 0 {
					supers = []TypeIdx{{Index: uint32(i - 1)}}
				}
				m.Types[i] = openStructType(nil, supers...)
			}
			v := &moduleValidator{m: m}
			v.ensureTypeIndex()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for child := 1; child < count; child++ {
					if !v.typeIdxSuperSubtype(TypeIdx{Index: uint32(child)}, TypeIdx{Index: 0}) {
						b.Fatal("missing supertype")
					}
				}
			}
		})
	}
}
