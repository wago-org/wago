//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceResourceDecode(t *testing.T, vector, tails, wide bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModuleByteBackedWithLimits(wasmtest.ClosedReferenceResources(vector, tails, wide), wasm.ValidationFeatures{ExtendedConstGlobals: true, MultiMemory: true}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return m.Module
}

func TestReferenceFunctionResourceExports(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-tail%t-wide%t", vector, tails, wide), func(t *testing.T) {
					m := referenceResourceDecode(t, vector, tails, wide)
					m.Exports[len(m.Exports)-1].Name = "" // A valid empty alias.
					image, err := CompileModuleWith(m, CompileOptions{NativeReferenceFunctions: true, NativeTailCalls: tails, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, DeferCodeMapping: true})
					if err != nil || !image.NativeReferenceFunctions || len(image.Entry) != 10 {
						t.Fatal("owned resource export compilation", err)
					}
					for _, mutate := range []func(*wasm.Module){
						func(v *wasm.Module) {
							v.Exports[len(v.Exports)-1].Index = wasm.ExternIdx{Kind: wasm.ExternMem, Index: 2}
						},
						func(v *wasm.Module) {
							v.Exports[len(v.Exports)-1].Index = wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 3}
						},
						func(v *wasm.Module) {
							v.Exports[len(v.Exports)-1].Index = wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}
						},
						func(v *wasm.Module) { v.Globals[2].Type.Type = wasm.FuncRef },
					} {
						m := referenceResourceDecode(t, vector, tails, wide)
						mutate(m)
						if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
							t.Fatal("resource confinement", err)
						}
					}
					m = referenceResourceDecode(t, vector, tails, wide)
					m.Exports[len(m.Exports)-1].Name = "run"
					if _, err := CompileModuleWith(m, CompileOptions{NativeReferenceFunctions: true, NativeTailCalls: tails, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, DeferCodeMapping: true}); err == nil {
						t.Fatal("duplicate cross-kind name escaped mandatory frontend validation")
					}
				})
			}
		}
		m := referenceResourceDecode(t, vector, false, false)
		for len(m.Exports) < MaxFunctions {
			m.Exports = append(m.Exports, wasm.Export{Name: fmt.Sprintf("alias%d", len(m.Exports)), Index: wasm.ExternIdx{Kind: wasm.ExternMem, Index: 1}})
		}
		if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
			t.Fatal("exact combined export count", err)
		}
		m.Exports = append(m.Exports, wasm.Export{Name: "overflow", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 1}})
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("combined export count overflow", err)
		}
		m = referenceResourceDecode(t, vector, false, false)
		buffer := make([]byte, MaxModuleBytes)
		m.Customs = []wasm.CustomSec{{Name: "opaque"}}
		lo, hi := 0, len(buffer)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			m.Customs[0].Data = buffer[:mid]
			if admitClosedReferenceFunctionModule(m, vector) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		m.Customs[0].Data = buffer[:lo]
		if lo == 0 || admitClosedReferenceFunctionModule(m, vector) != nil {
			t.Fatal("exact combined resource name budget")
		}
		m.Exports[len(m.Exports)-1].Name += "!"
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("resource name byte overflow", err)
		}
	}
}
