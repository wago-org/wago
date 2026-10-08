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

func TestReferenceNonNullPrivateCarrierGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"null", []byte{0xd0, 0x70, 0xd4, 0xd1, 0x1a, 0x0b}, true},
		{"repeat", []byte{0xd0, 0x70, 0xd4, 0xd4, 0x1a, 0x0b}, true},
		{"dead-polymorphic", []byte{0x00, 0xd4, 0xd4, 0xd1, 0x1a, 0x0b}, true},
		{"dead-known-ref", []byte{0x00, 0xd0, 0x70, 0xd4, 0x1a, 0x0b}, true},
		{"underflow", []byte{0xd4, 0x1a, 0x0b}, false},
		{"i32", []byte{0x41, 0, 0xd4, 0x1a, 0x0b}, false},
		{"i64", []byte{0x42, 0, 0xd4, 0x1a, 0x0b}, false},
		{"dead-known-i64", []byte{0x00, 0x42, 0, 0xd4, 0x1a, 0x0b}, false},
		{"preserve-ref-class", []byte{0xd0, 0x70, 0xd4, 0x50, 0x1a, 0x0b}, false},
		{"externref", []byte{0xd0, 0x6f, 0xd4, 0x1a, 0x0b}, false},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed%t", tc.name, enabled), func(t *testing.T) {
				fn := &wasm.Func{BodyBytes: tc.body}
				state := moduleState{production: true, importReferences: true, referenceCalls: enabled}
				_, a := walkInteger(fn, ft, &state, nil)
				_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, enabled, nil)
				for _, err := range []error{a, b} {
					if (err == nil) != (enabled && tc.ok) {
						t.Fatalf("scalar=%v SWAR=%v", a, b)
					}
				}
			})
		}
	}
}

// Private lowering must not turn ordinary call_ref's persisted marker into
// permission for another opcode, even in a module already containing call_ref.
func TestProductionReferenceNonNullRemainsClosed(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, dead := range []bool{false, true} {
			for _, call := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-dead%t-call%t", vector, dead, call), func(t *testing.T) {
					types := [][]byte{wasmtest.FuncType(nil, nil)}
					if vector {
						types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
					}
					body := []byte{0xd0, 0x70, 0xd4, 0x1a}
					if call {
						body = append(body, 0xd0, 0, 0x14, 0)
					}
					if dead {
						body = append([]byte{0x00}, body...)
					}
					body = append(body, 0x0b)
					source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					m, err := wasm.DecodeModule(source)
					if err != nil {
						t.Fatal(err)
					}
					if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
						t.Fatal("invalid gate fixture", err)
					}
					pressure := 0
					image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
					if image != nil || !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "ref.as_non_null artifact contract") || pressure != 0 {
						t.Fatal("private instruction reached production staging", image, err, pressure)
					}
				})
			}
		}
	}
}
