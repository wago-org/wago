package wago

import (
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func indexedGlobalModule(n int) []byte {
	return indexedGlobalClusterModule(n, 1)
}

func indexedGlobalClusterModule(n, cluster int) []byte {
	types := make([][]byte, n)
	globals := make([][]byte, 0, n*2)
	for i := 0; i < n; i++ {
		types[i] = []byte{0x60, 0, 0}
		typ := append([]byte{0x63}, wasmtest.SLEB32(int32(i))...)
		init := append([]byte{0xd0}, wasmtest.SLEB32(int32(i))...)
		g := append(append(typ, 0), init...)
		g = append(g, 0x0b)
		for repeat := 0; repeat < cluster; repeat++ {
			globals = append(globals, g)
		}
	}
	globals = append(globals, globals...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(6, wasmtest.Vec(globals...)))
}

func TestInternedGlobalDescriptorsRoundTrip(t *testing.T) {
	for _, n := range []int{1, 16, 17, 128} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), indexedGlobalModule(n))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if len(c.ValueTypes) != n {
				t.Fatalf("pool length = %d, want %d", len(c.ValueTypes), n)
			}
			for i, g := range c.Globals {
				if g.ValueTypeIndex != uint32(i%n) {
					t.Fatalf("global %d index = %d", i, g.ValueTypeIndex)
				}
			}
			data, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			// Core 3 admission remains compile-only; check metadata through the
			// private codec without widening the public artifact feature gate.
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, data[5:]); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			for i, g := range loaded.Globals {
				if g.ValueTypeIndex != uint32(i%n) {
					t.Fatalf("loaded global %d index = %d", i, g.ValueTypeIndex)
				}
			}
		})
	}
}

func BenchmarkCompileValueTypeInterning(b *testing.B) {
	for _, n := range []int{1, 4, 16, 128, 1024} {
		b.Run(fmt.Sprintf("distinct%d", n), func(b *testing.B) {
			source := indexedGlobalModule(n)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, source)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompileValueTypeClusters(b *testing.B) {
	for _, n := range []int{4, 128, 1024} {
		b.Run(fmt.Sprintf("distinct%d", n), func(b *testing.B) {
			source := indexedGlobalClusterModule(n, 16)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, source)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

var internMetadataSink uint32

func BenchmarkValueTypeMetadata(b *testing.B) {
	for _, n := range []int{4, 128, 1024} {
		for _, cluster := range []int{1, 16} {
			b.Run(fmt.Sprintf("distinct%d/cluster%d", n, cluster), func(b *testing.B) {
				descriptors := make([]ValueTypeDescriptor, n)
				for i := range descriptors {
					descriptors[i] = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(i)}}}
				}
				pool := make([]ValueTypeDescriptor, 0, n)
				b.ReportAllocs()
				b.ResetTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					pool = pool[:0]
					var cache valueTypeInterner
					var sum uint32
					for pass := 0; pass < 2; pass++ {
						for _, descriptor := range descriptors {
							for repeat := 0; repeat < cluster; repeat++ {
								sum += cache.intern(&pool, descriptor)
							}
						}
					}
					internMetadataSink = sum
				}
			})
		}
	}
}

func TestValueTypeInternerMatchesLinear(t *testing.T) {
	var pool, want []ValueTypeDescriptor
	var cache valueTypeInterner
	for pass := 0; pass < 3; pass++ {
		for i := 0; i < 128; i++ {
			for variant := 0; variant < 8; variant++ {
				d := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: variant&1 != 0, Exact: variant&2 != 0, Heap: HeapTypeDescriptor{Defined: variant&4 != 0, Abstract: AbstractHeapFunc, TypeIndex: uint32(i)}}}
				for repeat := 0; repeat < 3; repeat++ {
					gotIndex := cache.intern(&pool, d)
					wantIndex := internValueType(&want, d)
					if gotIndex != wantIndex {
						t.Fatalf("index = %d, want %d", gotIndex, wantIndex)
					}
				}
			}
		}
	}
	if len(pool) != len(want) {
		t.Fatalf("pool length = %d, want %d", len(pool), len(want))
	}
	for i := range pool {
		if pool[i] != want[i] {
			t.Fatalf("pool differs at %d", i)
		}
	}
}

func TestInternedGlobalClusters(t *testing.T) {
	for _, n := range []int{4, 128} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), indexedGlobalClusterModule(n, 16))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if len(c.ValueTypes) != n {
			t.Fatalf("pool length = %d, want %d", len(c.ValueTypes), n)
		}
		for i, global := range c.Globals {
			if global.ValueTypeIndex != uint32((i/16)%n) {
				t.Fatalf("global %d index = %d", i, global.ValueTypeIndex)
			}
		}
	}
}
