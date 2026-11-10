//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferencePolymorphicSelect(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        []byte
		ok, witness bool
	}{
		{"nonnull", []byte{0, 0x1b, 0xd4, 0x1a, 0x0b}, true, true},
		{"chained", []byte{0, 0x1b, 0x1b, 0x1b, 0xd4, 0x1a, 0x0b}, true, true},
		{"null-branch", []byte{0, 0x1b, 0xd5, 0, 0x1a, 0x0b}, true, true},
		{"nonnull-branch", []byte{2, 0x70, 0, 0x1b, 0xd6, 0, 0, 0x0b, 0x1a, 0x0b}, true, true},
		{"null-ref-prefix", []byte{2, 0x70, 0, 0x1b, 0xd0, 0x70, 0xd5, 0, 0x1a, 0xd4, 0x0b, 0x1a, 0x0b}, true, true},
		{"null-i64-prefix", []byte{2, 0x7e, 0, 0x1b, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x42, 0, 0x41, 0, 0x1b, 0x0b, 0x1a, 0x0b}, true, true},
		{"typed-ref", []byte{0, 0x1b, 0x41, 0, 0x1c, 1, 0x70, 0x1a, 0x0b}, true, false},
		{"typed-nullable", []byte{0, 0x1b, 0x41, 0, 0x1c, 1, 0x63, 0x70, 0x1a, 0x0b}, true, false},
		{"typed-nonnull", []byte{0, 0x1b, 0x41, 0, 0x1c, 1, 0x64, 0x70, 0x1a, 0x0b}, true, true},
		{"typed-i64", []byte{0, 0x1b, 0x41, 0, 0x1c, 1, 0x7e, 0x42, 0, 0x41, 0, 0x1b, 0x1a, 0x0b}, true, false},
		{"known-i64-arm", []byte{0, 0x1b, 0x42, 0, 0x41, 0, 0x1b, 0x42, 0, 0x41, 0, 0x1b, 0x1a, 0x0b}, true, false},
		{"virtual-left-arm", []byte{0, 0x42, 0, 0x41, 0, 0x1b, 0x42, 0, 0x41, 0, 0x1b, 0x1a, 0x0b}, true, false},
		{"known-i32-arm", []byte{0, 0x1b, 0x41, 0, 0x41, 0, 0x1b, 0x41, 0, 0x41, 0, 0x1b, 0x1a, 0x0b}, true, false},
		{"unknown-condition", []byte{0, 0x1b, 0x1b, 0x1a, 0x0b}, true, false},
		{"indexed-i64-control", []byte{0, 0x1b, 2, 1, 0x0b, 0x1a, 0x0b}, true, false},
		{"nested-explicit-dead", []byte{0, 2, 0x40, 0, 0x1b, 0xd4, 0x1a, 0x0b, 0x0b}, true, true},
		{"inherited-dead", []byte{0, 2, 0x40, 0x1b, 0x1a, 0x0b, 0x0b}, false, false},
		{"inherited-dead-one-arm", []byte{0, 2, 0x40, 0x42, 0, 0x41, 0, 0x1b, 0x1a, 0x0b, 0x0b}, false, false},
		{"known-ref-arm", []byte{0, 0xd0, 0x70, 0x41, 0, 0x1b, 0x1a, 0x0b}, false, false},
		{"known-left-ref-arm", []byte{0, 0xd0, 0x70, 0x42, 0, 0x41, 0, 0x1b, 0x1a, 0x0b}, false, false},
		{"known-wide-condition", []byte{0, 0x1b, 0x42, 0, 0x1b, 0x1a, 0x0b}, false, false},
		{"known-ref-condition", []byte{0, 0x1b, 0xd0, 0x70, 0x1b, 0x1a, 0x0b}, false, false},
		{"known-mismatched-arms", []byte{0, 0x41, 0, 0x42, 0, 0x41, 0, 0x1b, 0x1a, 0x0b}, false, false},
		{"known-arm-specializes", []byte{0, 0x1b, 0x42, 0, 0x41, 0, 0x1b, 0xd4, 0x1a, 0x0b}, false, true},
		{"typed-i64-specializes", []byte{0, 0x1b, 0x41, 0, 0x1c, 1, 0x7e, 0xd4, 0x1a, 0x0b}, false, true},
		{"ref-prefix-specializes", []byte{2, 0x70, 0, 0x1b, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x41, 0, 0x1b, 0x0b, 0x1a, 0x0b}, false, true},
		{"i64-prefix-specializes", []byte{2, 0x7e, 0, 0x1b, 0xd0, 0x70, 0xd5, 0, 0x1a, 0xd4, 0x0b, 0x1a, 0x0b}, false, true},
		{"typed-ref-specializes", []byte{0, 0x1b, 0x41, 0, 0x1c, 1, 0x70, 0x41, 0, 0x1b, 0x1a, 0x0b}, false, false},
		{"push-clears-unknown", []byte{0, 0x1b, 0x1a, 0x42, 0, 0xd4, 0x1a, 0x0b}, false, true},
		{"local-get-clears-unknown", []byte{0, 0x1b, 0x1a, 0x20, 0, 0xd4, 0x1a, 0x0b}, false, true},
		{"local-tee-clears-unknown", []byte{0, 0x1b, 0x22, 0, 0xd4, 0x1a, 0x0b}, false, true},
		{"join-clears-unknown", []byte{0, 2, 0x7e, 0, 0x1b, 0x0b, 0xd4, 0x1a, 0x0b}, false, true},
	} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-vector%t", tc.name, vector), func(t *testing.T) {
				types := [][]byte{wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64})}
				if vector {
					types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
				}
				code := append([]byte{1, 1, 0x7e}, tc.body...) // one i64 local for stale-class regressions
				code = append(wasmtest.ULEB(uint32(len(code))), code...)
				m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(code))))
				if err != nil {
					t.Fatal(err)
				}
				// The full frontend, independently of carrier erasure, is the semantic oracle.
				err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
				var invalid *wasm.ValidationError
				if tc.ok && err != nil || !tc.ok && (!errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch) {
					t.Fatal("frontend oracle", err)
				}
				ft := &m.Types[0].SubTypes[0].Comp
				sig := &m.Types[1].SubTypes[0].Comp
				state := moduleState{production: true, importReferences: true, referenceCalls: true, types: []functionSpec{{signature: ft}, {params: []bool{true}, results: []bool{true}, signature: sig}}}
				_, scalarErr := walkInteger(&m.Code[0], ft, &state, nil)
				_, swarErr := walkSWARPairsWithTypedCallMode(&m.Code[0], ft, memoryProfile{}, []*wasm.CompType{ft, sig}, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, nil)
				if (scalarErr == nil) != tc.ok || (swarErr == nil) != tc.ok {
					t.Fatal("private carrier admission", scalarErr, swarErr)
				}
				pressure := 0
				image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
				if !tc.ok {
					if image != nil || !errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch || pressure != 0 {
						t.Fatal("invalid source reached staging", image, err, pressure)
					}
				} else if err != nil || image == nil || image.NativeReferenceControl != tc.witness || image.NativeReferenceCalls || image.NativeTail != nil || image.CodeImage != nil || pressure != 1 {
					t.Fatal("polymorphic source/witness", image, err, pressure)
				}
			})
		}
	}
}
