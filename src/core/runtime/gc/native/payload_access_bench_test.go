package gc

import "testing"

// This uses only pre-existing collector APIs so exactly the same source can run
// against the refactor's parent. It isolates the default Throughput/Tiny boundary
// cost; it is not a Wasm/V8 or whole-collector benchmark.
func BenchmarkPayloadBoundary(b *testing.B) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		name := "throughput"
		if profile == ProfileTiny {
			name = "tiny"
		}
		b.Run(name, func(b *testing.B) {
			for _, op := range []string{"struct-get", "struct-set", "array-get", "array-set", "struct-new", "array-new", "array-borrow"} {
				b.Run(op, func(b *testing.B) {
					sd, _ := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
					sd.Final = true
					ad, _ := NewArrayDesc(1, StorageI32)
					ad.Final = true
					c, e := NewCollector(Config{Profile: profile, NurseryBytes: 1 << 20, ThroughputHeapBytes: 8 << 20, TinyHeapBytes: 1 << 20}, []TypeDesc{sd, ad})
					if e != nil {
						b.Fatal(e)
					}
					defer c.Close()
					st, e := c.NewStructDefault(0)
					if e != nil {
						b.Fatal(e)
					}
					ar, e := c.NewArrayDefault(1, 16)
					if e != nil {
						b.Fatal(e)
					}
					values := []Value{I32Value(7), I64Value(99)}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var v Value
						var e error
						switch op {
						case "struct-get":
							v, _, _, e = c.StructGetTyped(st, 0, true, 1)
						case "struct-set":
							_, _, e = c.StructSetTyped(st, 0, true, 1, I64Value(int64(i)))
						case "array-get":
							v, _, _, e = c.ArrayGetTyped(ar, 1, true, uint32(i&15))
						case "array-set":
							_, _, e = c.ArraySetTyped(ar, 1, true, uint32(i&15), I32Value(int32(i)))
						case "struct-new":
							_, e = c.NewStructWithRoots(0, values, EmptyRoots{})
						case "array-new":
							_, e = c.NewArrayWithRoots(1, 16, I32Value(int32(i)), EmptyRoots{})
						case "array-borrow":
							var view ArrayPayloadView
							view, e = c.ArrayPayload(ar)
							objectAccessBenchSink = uint64(len(view.Bytes))
						}
						if e != nil {
							b.Fatal(e)
						}
						if op != "array-borrow" {
							objectAccessBenchSink = v.Bits
						}
					}
				})
			}
		})
	}
}
