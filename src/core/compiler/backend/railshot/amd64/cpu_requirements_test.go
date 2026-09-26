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

func TestDisabledVEXMemoryOptimizationRequirements(t *testing.T) {
	for _, into := range []bool{false, true} {
		body := []byte{0, 0x20, 0, 0x20, 1, 0x2b, 3, 0, 0xa0}
		if into {
			body = append(body, 0x21, 0, 0x20, 0)
		}
		body = append(body, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, body)
		for _, enabled := range []bool{false, true} {
			cm, err := CompileModuleWith(m, CompileOptions{
				AMD64FeaturesSet: true, AMD64Features: shared.AMD64AVX,
				Optimizations: map[string]bool{"vex-float-mem": enabled},
			})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			want := uint32(0)
			if enabled {
				want = uint32(shared.AMD64AVX)
			}
			if cm.RequiredAMD64Features != want {
				t.Fatalf("into=%v enabled=%v requirements=%x want=%x", into, enabled, cm.RequiredAMD64Features, want)
			}
		}
	}
}
