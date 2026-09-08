package wago

import (
	"fmt"
	"testing"
)

var benchmarkImportIndex map[string]importBindingKey

func BenchmarkImportIdentityIndex(b *testing.B) {
	for _, count := range []int{0, 1, 4, 8, 16, 64, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			specs := make([]ImportSpec, count)
			for i := range specs {
				specs[i] = ImportSpec{Module: "env.prod", Name: fmt.Sprintf("f%d", i)}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				index, err := indexDeclaredImportIdentities(specs)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkImportIndex = index
			}
		})
	}
}

func TestSmallImportIdentityComparisonMatchesFlatNamespace(t *testing.T) {
	names := []string{"", "a", "b", ".", "a.b", "a.", ".."}
	for _, am := range names {
		for _, an := range names {
			for _, bm := range names {
				for _, bn := range names {
					a, b := importBindingKey{am, an}, importBindingKey{bm, bn}
					if got, want := sameFlattenedImport(a, b), am+"."+an == bm+"."+bn; got != want {
						t.Fatalf("flat comparison %+v %+v: %v != %v", a, b, got, want)
					}
				}
			}
		}
	}
	for _, count := range []int{4, 5} {
		specs := make([]ImportSpec, count)
		for i := range specs {
			specs[i] = ImportSpec{Module: "env", Name: fmt.Sprint(i)}
		}
		specs[0] = ImportSpec{Module: "env.a", Name: "b"}
		specs[1] = ImportSpec{Module: "env", Name: "a.b"}
		if _, err := indexDeclaredImportIdentities(specs); err == nil {
			t.Fatalf("%d-row index accepted an ambiguous identity", count)
		}
	}
}
