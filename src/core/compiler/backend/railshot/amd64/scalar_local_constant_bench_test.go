//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func scalarLocalConstantModule(t testing.TB) *wasm.Module {
	body := []byte{1, 1, 0x7f}
	for i := 0; i < 1000; i++ {
		body = append(body, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0)
	}
	body = append(body, 0x20, 0, 0x0b)
	return modFuncs(t, funcDef{results: []wasm.ValType{wasm.I32}, body: body})
}
func TestScalarLocalConstantChain(t *testing.T) {
	m := scalarLocalConstantModule(t)
	for _, compact := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, CompactNative: compact, Optimizations: map[string]bool{"reg-abi": regABI}})
			if err != nil {
				t.Fatal(err)
			}
			if got := runCompiledAmd64u(t, cm); got != 1000 {
				t.Fatalf("got=%d", got)
			}
			t.Logf("compact=%v regABI=%v nativeBytes=%d", compact, regABI, len(cm.Code))
			if cm.CodeImage != nil {
				if err := cm.CodeImage.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
func BenchmarkCompileScalarLocalConstants(b *testing.B) {
	m := scalarLocalConstantModule(b)
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
}

func BenchmarkCompileScalarLocalVariable(b *testing.B) {
	body := []byte{0}
	for i := 0; i < 1000; i++ {
		body = append(body, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0)
	}
	body = append(body, 0x20, 0, 0x0b)
	m := modFuncs(b, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
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
}
