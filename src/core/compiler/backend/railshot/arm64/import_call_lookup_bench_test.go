//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func importCallLookupModule(tb testing.TB, imports, calls int) *wasm.Module {
	tb.Helper()
	entries := make([][]byte, imports)
	for i := range entries {
		entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("f%d", i))...)
		entries[i] = append(entry, 0, 0)
	}
	body := []byte{0}
	target := uint32(0)
	if imports > 0 {
		target = uint32(imports - 1)
	}
	for i := 0; i < calls; i++ {
		body = append(body, 0x10)
		body = append(body, wasmtest.ULEB(target)...)
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(entries...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		tb.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func BenchmarkCompileImportCallLookup(b *testing.B) {
	for _, imports := range []int{0, 1, 128, 1024} {
		for _, calls := range []int{1, 8192} {
			b.Run(fmt.Sprintf("imports=%d/calls=%d", imports, calls), func(b *testing.B) {
				m := importCallLookupModule(b, imports, calls)
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
}

func TestCompileImportCallLookupFixtures(t *testing.T) {
	for _, imports := range []int{0, 1, 128, 1024} {
		for _, calls := range []int{1, 8192} {
			m := importCallLookupModule(t, imports, calls)
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
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
}
