//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64SWARBranchHintModule(withHint bool, direction byte) []byte {
	body := []byte{0x41, 8, 0x20, 0, 0x0d, 0, 0x1a, 0x41, 9, 0x0b}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0), wasmtest.ExportEntry("vector", 0, 1))),
	}
	if withHint {
		payload := append(wasmtest.Name("metadata.code.branch_hint"), 1, 0, 1, 5, 1, direction)
		sections = append(sections, wasmtest.Section(0, payload))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code(body),
		wasmtest.Code([]byte{0x20, 0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicBranchHints(t *testing.T) {
	for _, edge := range []string{"return", "loop", "swar-return"} {
		for _, direction := range []byte{0, 1} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-direction%d-artifact%t", edge, direction, artifact), func(t *testing.T) {
					var plainSource, hintedSource []byte
					if edge == "return" {
						plainSource = branchHintReturnModuleDirection(false, direction)
						hintedSource = branchHintReturnModuleDirection(true, direction)
					} else if edge == "loop" {
						plainSource = branchHintLoopModuleDirection(false, direction)
						hintedSource = branchHintLoopModuleDirection(true, direction)
					} else {
						plainSource = rv64SWARBranchHintModule(false, direction)
						hintedSource = rv64SWARBranchHintModule(true, direction)
					}
					key := fmt.Sprintf("branch-hint-%s-direction%d", edge, direction)
					plain := rv64TableStageCompile(t, key+"-plain", plainSource, artifact)
					hinted := rv64TableStageCompile(t, key+"-hinted", hintedSource, artifact)
					if !bytes.Equal(plain.code, hinted.code) || !slices.Equal(plain.Entry, hinted.Entry) || !slices.Equal(plain.InternalEntry, hinted.InternalEntry) {
						t.Fatal("advisory branch hint changed RV64 code layout")
					}
					rt := NewRuntime()
					defer rt.Close()
					instances := []*Instance{
						rv64NativeTokenInstantiate(t, rt, plain, NewImports()),
						rv64NativeTokenInstantiate(t, rt, hinted, NewImports()),
					}
					plain.Close()
					hinted.Close()
					for _, in := range instances {
						if edge != "loop" {
							importedGlobalInvoke(t, in, "f", []uint64{9}, 0xdeadbeef00000000)
							importedGlobalInvoke(t, in, "f", []uint64{8}, 0xdeadbeef00000001)
						} else {
							importedGlobalInvoke(t, in, "f", []uint64{0}, 1)
							importedGlobalInvoke(t, in, "f", []uint64{0}, 0xdeadbeef00000003)
						}
						in.Close()
					}
				})
			}
		}
	}
}
