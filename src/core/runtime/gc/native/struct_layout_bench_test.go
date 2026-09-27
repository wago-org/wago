package gc

import (
	"fmt"
	"testing"
)

func BenchmarkValidateStructLayout(b *testing.B) {
	for _, count := range []int{128, 8192} {
		for _, layout := range []string{"ordered", "reversed", "sparse"} {
			b.Run(fmt.Sprintf("%s/fields=%d", layout, count), func(b *testing.B) {
				kinds := make([]StorageKind, count)
				for i := range kinds {
					kinds[i] = StorageI32
				}
				desc, err := NewStructDesc(0, kinds)
				if err != nil {
					b.Fatal(err)
				}
				if layout == "sparse" {
					for i := range desc.Fields {
						desc.Fields[i].Offset = uint32(i) * 4096
					}
					desc.Size = desc.Fields[len(desc.Fields)-1].Offset + 4
				}
				if layout != "ordered" {
					for i, j := 0, len(desc.Fields)-1; i < j; i, j = i+1, j-1 {
						desc.Fields[i], desc.Fields[j] = desc.Fields[j], desc.Fields[i]
					}
				}
				descs := []TypeDesc{desc}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := ValidateTypeDescs(descs); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
