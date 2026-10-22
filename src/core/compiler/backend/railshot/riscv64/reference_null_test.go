//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceNullModule(t *testing.T, vector, dead bool, immediate []byte) *wasm.Module {
	t.Helper()
	types := make([][]byte, 65)
	for i := range types {
		types[i] = wasmtest.FuncType(nil, nil)
	}
	if vector {
		types[64] = wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil)
	}
	body := []byte{}
	if dead {
		body = append(body, 0)
	}
	body = append(body, 0xd0)
	body = append(body, immediate...)
	body = append(body, 0xd1, 0x1a, 0x0b)
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestProductionReferenceNullContract(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, dead := range []bool{false, true} {
			for _, heap := range []int32{-16, 0, 64} {
				for _, permission := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-dead%t-heap%d-permission%t", vector, dead, heap, permission), func(t *testing.T) {
						m := referenceNullModule(t, vector, dead, wasmtest.SLEB32(heap))
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
							t.Fatal("fixture", err)
						}
						pressure := 0
						image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: permission, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
						if heap >= 0 && !permission {
							if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
								t.Fatal("missing indexed null permission", image, err, pressure)
							}
						} else if err != nil || image == nil || image.NativeReferenceControl != (heap >= 0) || image.NativeReferenceCalls || image.NativeTail != nil || image.CodeImage != nil || pressure != 1 {
							t.Fatal("null-only witness", image, err, pressure)
						}
					})
				}
			}
		}
	}
}

func TestProductionReferenceNullRejectedBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, dead := range []bool{false, true} {
			for _, tc := range []struct {
				name string
				heap []byte
			}{
				{"out-of-range", wasmtest.SLEB32(65)},
				{"extern", []byte{0x6f}},
				{"any", []byte{0x6e}},
				{"exact", []byte{0x62, 0}},
				{"truncated", []byte{0x80}},
			} {
				t.Run(fmt.Sprintf("vector%t-dead%t-%s", vector, dead, tc.name), func(t *testing.T) {
					m := referenceNullModule(t, vector, dead, tc.heap)
					if tc.name == "truncated" {
						m.Code[0].BodyBytes = []byte{0xd0, 0x80}
					}
					if tc.name == "extern" || tc.name == "any" {
						checkPublicPrimitiveModule(t, m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true})
						return
					}
					pressure := 0
					image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
					if image != nil || err == nil || pressure != 0 {
						t.Fatal("unsupported null staged", image, err, pressure)
					}
				})
			}
		}
	}
}

func TestProductionReferenceNullTailPermission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tail := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-tail%t", vector, tail), func(t *testing.T) {
				m := referenceNullModule(t, vector, false, []byte{0})
				if tail {
					m.Code[0].BodyBytes = []byte{0xd0, 0, 0x15, 0, 0x0b}
				}
				pressure := 0
				// NativeTailCalls historically permits indexed nulls without the
				// separate ordinary-call flag. Preserve both that null-only path
				// and its composition with return_call_ref.
				image, err := CompileModuleWith(m, CompileOptions{NativeTailCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
				if err != nil || image == nil || !image.NativeReferenceControl || image.NativeReferenceCalls || image.CodeImage != nil || pressure != 1 || tail && image.NativeTail == nil {
					t.Fatal("tail-only indexed null permission", image, err, pressure)
				}
			})
		}
	}
}
