//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProductionReferenceCallContract(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, dead := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-dead%t", vector, dead), func(t *testing.T) {
				typ := wasm.I32
				if vector {
					typ = wasm.V128
				}
				sig := []wasm.ValType{typ}
				body := []byte{0x20, 0, 0xd2, 0, 0x14, 1, 0x0b}
				if dead {
					body = append([]byte{0x00}, body...)
				}
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig), wasmtest.FuncType(sig, sig))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code(body))))
				m, err := wasm.DecodeModule(source)
				if err != nil {
					t.Fatal(err)
				}
				opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, NativeReferenceCalls: true}
				first, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				second, err := CompileModuleWith(m, opts)
				if err != nil || !first.NativeReferenceCalls || first.NativeReferenceControl || first.NativeTail != nil || first.CodeImage != nil || len(first.Code) == 0 || !bytes.Equal(first.Code, second.Code) {
					t.Fatal("address-free reference contract", err)
				}
				for _, missing := range []string{"permission", "invocation", "interruptible", "foreign"} {
					broken := opts
					switch missing {
					case "permission":
						broken.NativeReferenceCalls = false
					case "invocation":
						broken.InvocationContext = false
					case "interruptible":
						broken.Interruptible = false
					case "foreign":
						broken.ForeignTableCalls = false
					}
					pressure := 0
					broken.MemoryPressure = func() { pressure++ }
					if image, err := CompileModuleWith(m, broken); image != nil || err == nil || pressure != 0 {
						t.Fatal("incomplete reference ABI reached staging", missing, image, err, pressure)
					}
				}
				if image, err := CompileWithOptions(source, CompileOptions{NativeReferenceCalls: true}); image != nil || err == nil {
					t.Fatal("isolated reference option admitted", err)
				}
			})
		}
	}
}

func TestProductionReferenceCallDeadCountBoundary(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, count := range []int{maxDirectCalls, maxDirectCalls + 1} {
			t.Run(fmt.Sprintf("vector%t-count%d", vector, count), func(t *testing.T) {
				types := [][]byte{wasmtest.FuncType(nil, nil)}
				if vector {
					types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
				}
				body := []byte{0x00}
				for range count {
					body = append(body, 0xd0, 0, 0x14, 0)
				}
				body = append(body, 0x0b)
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				m, err := wasm.DecodeModule(source)
				if err != nil {
					t.Fatal(err)
				}
				pressure := 0
				image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
				if count == maxDirectCalls {
					if err != nil || image == nil || !image.NativeReferenceCalls || !image.NativeReferenceControl || image.NativeTail != nil || pressure != 1 {
						t.Fatal("exact unreachable call_ref boundary", err, pressure)
					}
				} else if image != nil || err == nil || pressure != 0 {
					t.Fatal("oversized unreachable call_ref reached staging", err, pressure)
				}
			})
		}
	}
}
