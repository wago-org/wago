package gc

import "testing"

var typedRefBenchmarkMatch bool

// This measures the collector prototype only. It does not measure native Wasm
// lowering, the cost of heap-load reconstruction in a guest, or self-compilation.
// The mint case charges reconstruction inside the measured loop.
func BenchmarkDense16Prototype(b *testing.B) {
	benchmarkDense16Prototype(b, []string{"checked", "carried", "mint-carried", "unknown"})
}

func BenchmarkDense16PrototypeReverse(b *testing.B) {
	benchmarkDense16Prototype(b, []string{"unknown", "mint-carried", "carried", "checked"})
}

func benchmarkDense16Prototype(b *testing.B, implementations []string) {
	for _, kind := range []struct {
		name string
		kind RefTestKind
	}{
		{"exact", RefTestDefined},
		{"open", RefTestDefined},
		{"struct", RefTestStruct},
		{"any", RefTestAny},
	} {
		for _, implementation := range implementations {
			b.Run(kind.name+"/"+implementation, func(b *testing.B) {
				descs := make([]TypeDesc, 3501)
				for i := range descs {
					d, err := NewStructDesc(TypeID(i), []StorageKind{StorageI32})
					if err != nil {
						b.Fatal(err)
					}
					d.GuestType = true
					if i == 0 {
						d.Final = false
					} else {
						d.HasSuper, d.Super = true, 0
					}
					descs[i] = d
				}
				c, err := NewCollector(Config{}, descs)
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				const count = 1024
				var refs [count]Ref
				var values [count]TypedRef
				var targets [count]RefTestTarget
				for i := range refs {
					refs[i], err = c.NewStructDefaultWithRoots(TypeID(i+1), RefSliceRoots(refs[:]))
					if err != nil {
						b.Fatal(err)
					}
					values[i], err = c.TypedReference(refs[i])
					if err != nil {
						b.Fatal(err)
					}
					targets[i] = RefTestTarget{Kind: kind.kind}
					if kind.name == "exact" {
						targets[i].Type, targets[i].Exact = TypeID(i+1), true
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				match := false
				switch implementation {
				case "checked":
					for n := 0; n < b.N; n++ {
						i := n & (count - 1)
						match, err = c.RefTest(refs[i], targets[i])
						if err != nil {
							b.Fatal(err)
						}
					}
				case "carried":
					for n := 0; n < b.N; n++ {
						i := n & (count - 1)
						match, err = c.RefTestTyped(values[i], targets[i])
						if err != nil {
							b.Fatal(err)
						}
					}
				case "mint-carried":
					for n := 0; n < b.N; n++ {
						i := n & (count - 1)
						value, mintErr := c.TypedReference(refs[i])
						if mintErr != nil {
							b.Fatal(mintErr)
						}
						match, err = c.RefTestTyped(value, targets[i])
						if err != nil {
							b.Fatal(err)
						}
					}
				case "unknown":
					for n := 0; n < b.N; n++ {
						i := n & (count - 1)
						match, err = c.RefTestTyped(UnknownTypedRef(refs[i]), targets[i])
						if err != nil {
							b.Fatal(err)
						}
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(len(c.typeWords))*4, "typeword-table-B")
				b.ReportMetric(float64(cap(c.typeWords))*4, "typeword-cap-B")
				if !match {
					b.Fatal("valid fixture did not match")
				}
				typedRefBenchmarkMatch = match
			})
		}
	}
}
