//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceTypesPrivateGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, tc := range []struct {
		name      string
		body      []byte
		typed, ok bool
	}{
		{"nullable-block", []byte{2, 0x63, 0x70, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, false, true},
		{"nonnull-block", []byte{2, 0x64, 0x70, 0xd0, 0x70, 0xd4, 0x0b, 0x1a, 0x0b}, true, true},
		{"nonnull-if", []byte{0x41, 1, 4, 0x64, 0x70, 0xd0, 0x70, 0xd4, 5, 0xd0, 0x70, 0xd4, 0x0b, 0x1a, 0x0b}, true, true},
		{"nonnull-permission", []byte{2, 0x64, 0x70, 0, 0x0b, 0x1a, 0x0b}, false, false},
		{"extern-block", []byte{2, 0x63, 0x6f, 0, 0x0b, 0x1a, 0x0b}, true, false},
		{"indexed-block", []byte{2, 0x64, 0, 0, 0x0b, 0x1a, 0x0b}, true, false},
		{"missing-heap", []byte{2, 0x64}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := &wasm.Func{BodyBytes: tc.body}
			state := moduleState{production: true, importReferences: true, referenceCalls: tc.typed}
			_, a := walkInteger(fn, ft, &state, nil)
			_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, tc.typed, nil)
			if (a == nil) != tc.ok || (b == nil) != tc.ok {
				t.Fatal("explicit block type gate", a, b)
			}
		})
	}
	for _, nullable := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, heap := range []wasm.HeapType{wasm.AbsHeap(wasm.HeapFunc), wasm.AbsHeap(wasm.HeapExtern), wasm.IndexedHeap(wasm.TypeIdx{Index: 0})} {
				for _, exact := range []bool{false, true} {
					typ := wasm.RefVal(wasm.Ref(nullable, heap, exact))
					want := !exact && heap == wasm.AbsHeap(wasm.HeapFunc) && (nullable || enabled)
					fn := &wasm.Func{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 1, Type: typ}}}, BodyBytes: []byte{0x0b}}
					state := moduleState{production: true, importReferences: true, referenceCalls: enabled}
					_, a := walkInteger(fn, ft, &state, nil)
					_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, enabled, nil)
					if (a == nil) != want || (b == nil) != want {
						t.Fatal("internal local type gate", typ, enabled, a, b)
					}
				}
			}
		}
	}
}

func referenceTypesModule(t *testing.T, body []byte, local bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))))
	if err != nil {
		t.Fatal(err)
	}
	if local {
		m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false))}}
	}
	return m
}

func TestReferenceTypesFrontendInitialization(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"get-before-set", []byte{0x20, 0, 0x1a, 0x0b}, false},
		{"nullable-assignment", []byte{0xd0, 0x70, 0x21, 0, 0x0b}, false},
		{"nested-initialization", []byte{2, 0x40, 0xd0, 0x70, 0xd4, 0x21, 0, 0x0b, 0x20, 0, 0x1a, 0x0b}, false},
		{"set", []byte{0xd0, 0x70, 0xd4, 0x21, 0, 0x20, 0, 0x1a, 0x0b}, true},
		{"tee", []byte{0xd0, 0x70, 0xd4, 0x22, 0, 0x1a, 0x20, 0, 0x1a, 0x0b}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := referenceTypesModule(t, tc.body, true)
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			if (err == nil) != tc.ok {
				t.Fatal("frontend initialization proof", err)
			}
		})
	}
	for _, form := range []byte{0x63, 0x64} {
		m := referenceTypesModule(t, []byte{2, form, 0x70, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, false)
		err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
		if (err == nil) != (form == 0x63) {
			t.Fatal("frontend block nullability", form, err)
		}
	}
}

func TestProductionReferenceTypesRemainClosed(t *testing.T) {
	for _, local := range []bool{false, true} {
		for _, form := range []byte{0x63, 0x64} {
			for _, dead := range []bool{false, true} {
				t.Run(fmt.Sprintf("local%t-form%x-dead%t", local, form, dead), func(t *testing.T) {
					body := []byte{2, form, 0x70, 0xd0, 0x70, 0xd4, 0x0b, 0x1a, 0x0b}
					if local {
						body = []byte{0xd0, 0x70, 0xd4, 0x21, 0, 0x20, 0, 0x1a, 0x0b}
					}
					if dead {
						body = append([]byte{0}, body...)
					}
					m := referenceTypesModule(t, body, local)
					if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
						t.Fatal("fixture", err)
					}
					pressure := 0
					image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
					if image != nil || !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "artifact contract is pending") || pressure != 0 {
						t.Fatal("explicit type reached production staging", image, err, pressure)
					}
				})
			}
		}
	}
}
