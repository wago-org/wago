//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func functionTypeLookupModule(tb testing.TB, count int) *wasm.Module {
	return functionTypeLookupModuleCounts(tb, count, count)
}
func functionTypeLookupModuleCounts(tb testing.TB, groups, count int) *wasm.Module {
	tb.Helper()
	types, funcs, bodies := make([][]byte, groups), make([][]byte, count), make([][]byte, count)
	for i := range types {
		types[i] = wasmtest.FuncType(nil, nil)
	}
	for i := range funcs {
		index := i
		if groups != count {
			index = groups - 1
		}
		funcs[i] = wasmtest.ULEB(uint32(index))
		bodies[i] = wasmtest.Code([]byte{0x0b})
	}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		tb.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func BenchmarkCompileFunctionTypeLookup(b *testing.B) {
	for _, count := range []int{1, 8, 64, 512, 4096, 8192} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := functionTypeLookupModule(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					if err := cm.CodeImage.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
func BenchmarkCompileFunctionTypeLookupFunctionLight(b *testing.B) {
	for _, count := range []int{0, 1, 8} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := functionTypeLookupModuleCounts(b, 4096, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					if err := cm.CodeImage.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
func TestCompileFunctionTypeLookupFixtures(t *testing.T) {
	for _, count := range []int{1, 8, 64, 512, 4096, 8192} {
		cm, err := CompileModuleWith(functionTypeLookupModule(t, count), CompileOptions{Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			if err := cm.CodeImage.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func functionTypeLookupMixedModule(tb testing.TB, groups int) *wasm.Module {
	tb.Helper()
	types, funcs, bodies := make([][]byte, groups), make([][]byte, groups), make([][]byte, groups)
	flat := make([]uint32, groups)
	next := uint32(0)
	for i := range types {
		width := i%4 + 1
		flat[i] = next
		next += uint32(width)
		members := make([][]byte, width)
		for j := range members {
			members[j] = wasmtest.FuncType(nil, nil)
		}
		types[i] = append([]byte{0x4e}, wasmtest.Vec(members...)...)
	}
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(flat[(i*4051)%groups])
		bodies[i] = wasmtest.Code([]byte{0x0b})
	}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func BenchmarkCompileFunctionTypeLookupMixed(b *testing.B) {
	for _, groups := range []int{512, 4096, 8192} {
		b.Run(fmt.Sprint(groups), func(b *testing.B) {
			m := functionTypeLookupMixedModule(b, groups)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					if err := cm.CodeImage.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func TestCompileFunctionTypeLookupMixedFixtures(t *testing.T) {
	for _, groups := range []int{65, 81, 129, 512} {
		m := functionTypeLookupMixedModule(t, groups)
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("groups=%d/workers=%d", groups, workers), func(t *testing.T) {
				cm, err := CompileModuleWith(m, CompileOptions{Workers: workers})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					if err := cm.CodeImage.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
