//go:build linux && amd64 && !tinygo

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestEmittedAMD64Requirements(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, profile := range []shared.AMD64Features{0, shared.AMD64SSE41, shared.AMD64ModernBaseline} {
			m := modFuncs(t,
				funcDef{params: []wasm.ValType{wasm.F64}, results: []wasm.ValType{wasm.F64}, body: []byte{0, 0x20, 0, 0x9e, 0x0b}},
				funcDef{params: []wasm.ValType{wasm.F64, wasm.F64}, results: []wasm.ValType{wasm.F64}, body: []byte{0, 0x20, 0, 0x20, 1, 0xa0, 0x0b}},
			)
			cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile, Workers: workers})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			want := profile & (shared.AMD64SSE41 | shared.AMD64AVX)
			if cm.RequiredAMD64Features != uint32(want) {
				t.Fatalf("workers=%d profile=%x required=%x want=%x", workers, profile, cm.RequiredAMD64Features, want)
			}
			// Reuse pooled scratch with a scalar integer module, checking that the
			// previous compilation's optional requirements cannot leak into it.
			simple := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x0b})
			cm, err = CompileModuleWith(simple, CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if cm.RequiredAMD64Features != 0 {
				t.Fatalf("pure integer module requires %x", cm.RequiredAMD64Features)
			}
		}
	}
}
