//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceSelectClassesAndFloors(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, tc := range []struct {
		name   string
		prefix []byte
		tail   []byte
		ok     bool
	}{
		{"ordinary", []byte{0xd0, 0x70, 0xd0, 0x70, 0x41, 0}, nil, true},
		{"explicit-dead", []byte{0}, nil, true},
		{"dead-known-condition", []byte{0, 0x41, 0}, nil, true},
		{"missing-live", nil, nil, false},
		{"inherited-dead", []byte{0, 2, 0x40}, []byte{0x0b}, false},
		{"nested-explicit-dead", []byte{0, 2, 0x40, 0}, []byte{0x0b}, true},
		{"wide-condition", []byte{0xd0, 0x70, 0xd0, 0x70, 0x42, 0}, nil, false},
		{"dead-wide-condition", []byte{0, 0x42, 0}, nil, false},
		{"bad-arm", []byte{0x42, 0, 0xd0, 0x70, 0x41, 0}, nil, false},
		{"dead-bad-arm", []byte{0, 0x42, 0, 0x41, 0}, nil, false},
		{"cross-floor", []byte{0xd0, 0x70, 2, 0x40, 0xd0, 0x70, 0x41, 0}, []byte{0x0b, 0x1a}, false},
	} {
		for _, form := range []byte{0x70, 0x63, 0x64} {
			t.Run(fmt.Sprintf("%s-form%x", tc.name, form), func(t *testing.T) {
				body := append([]byte{}, tc.prefix...)
				body = append(body, 0x1c, 1, form)
				if form != 0x70 {
					body = append(body, 0x70)
				}
				body = append(body, 0x1a)
				body = append(body, tc.tail...)
				body = append(body, 0x0b)
				fn := &wasm.Func{BodyBytes: body}
				state := moduleState{production: true, importReferences: true, referenceCalls: true}
				_, a := walkInteger(fn, ft, &state, nil)
				_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, nil)
				if (a == nil) != tc.ok || (b == nil) != tc.ok {
					t.Fatal("select class/floor", a, b)
				}
			})
		}
	}
	for _, immediate := range [][]byte{{0}, {2, 0x70, 0x70}, {1, 0x40}, {1, 0}, {1, 0x63, 0x6f}, {1, 0x64, 0}, {1, 0x64}} {
		body := append([]byte{0, 0x1c}, immediate...)
		body = append(body, 0x1a, 0x0b)
		fn := &wasm.Func{BodyBytes: body}
		state := moduleState{production: true, importReferences: true, referenceCalls: true}
		_, a := walkInteger(fn, ft, &state, nil)
		_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, nil)
		if a == nil || b == nil {
			t.Fatal("invalid select immediate", immediate, a, b)
		}
	}
}

func TestProductionReferenceSelectContract(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, form := range []byte{0x70, 0x63, 0x64} {
			for _, permission := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-form%x-permission%t", vector, form, permission), func(t *testing.T) {
					body := []byte{0, 0x1c, 1, form}
					if form != 0x70 {
						body = append(body, 0x70)
					}
					body = append(body, 0x1a, 0x0b)
					types := [][]byte{wasmtest.FuncType(nil, nil)}
					if vector {
						types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
					}
					m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))))
					if err != nil {
						t.Fatal(err)
					}
					if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
						t.Fatal("fixture", err)
					}
					pressure := 0
					image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: permission, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
					if form == 0x64 && !permission {
						if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
							t.Fatal("missing select permission", image, err, pressure)
						}
					} else if err != nil || image == nil || image.NativeReferenceControl != (form == 0x64) || image.NativeReferenceCalls || image.NativeTail != nil || image.CodeImage != nil || pressure != 1 {
						t.Fatal("select-only witness", image, err, pressure)
					}
				})
			}
		}
	}
}
