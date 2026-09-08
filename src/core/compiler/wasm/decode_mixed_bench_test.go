package wasm

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// One implicit group among explicit groups measures the reason to keep the
// initial singleton slab small rather than reserve for all remaining groups.
func sparseImplicitTypeModule(groups int) []byte {
	payload := binary.AppendUvarint(nil, uint64(groups))
	payload = append(payload, 0x60, 0, 0)
	for i := 1; i < groups; i++ {
		payload = append(payload, 0x4e, 1, 0x60, 0, 0)
	}
	return module(section(secType, payload...))
}
func BenchmarkDecodeMixedTypeGroups(b *testing.B) {
	for _, groups := range []int{1000, 100000} {
		b.Run(fmt.Sprint(groups), func(b *testing.B) {
			data := sparseImplicitTypeModule(groups)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m, err := DecodeModule(data)
				if err != nil {
					b.Fatal(err)
				}
				if len(m.Types) != groups {
					b.Fatalf("decoded %d groups, want %d", len(m.Types), groups)
				}
			}
		})
	}
}
