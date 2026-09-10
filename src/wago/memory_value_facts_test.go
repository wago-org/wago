//go:build (linux && amd64) || ((linux || darwin) && arm64)

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Exercise the physical address carrier across calls, local homes and joins.
// The same result and trap oracle is used with value facts enabled and disabled.
func TestMemoryValueFactsAcrossTransfers(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, count := range []int{63, 64, 65} {
		for _, shape := range []string{"spill-call", "call-result", "join", "import-global"} {
			body := []byte{2, byte(count - 3), 0x7f, 1, 0x7c} // two parameters; mixed i32/f64 locals
			// Keep a non-constant FP local live across the call or join. Reading
			// it after the load makes a lost mixed-class snapshot visible.
			body = append(body, 0x20, 1, 0xb8, 0x21, byte(count-1)) // f64.convert_i32_u
			switch shape {
			case "spill-call":
				body = append(body, 0x20, 0, 0x41, 0, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x10, 1, 0x1a)
			case "call-result":
				body = append(body, 0x20, 0, 0x10, 1)
			case "join":
				body = append(body, 0x20, 1, 0x04, 0x7f, 0x20, 0, 0x41, 0, 0x6a, 0x05, 0x20, 0, 0x0b)
			case "import-global":
				body = append(body, 0x23, 0)
			}
			// load + restored FP choice - original integer choice must equal load.
			body = append(body, 0x28, 2, 0, 0x20, byte(count-1), 0xab, 0x6a, 0x20, 1, 0x6b, 0x0b)
			helper := append([]byte{0}, bytes.Repeat([]byte{0x01}, 200)...)
			helper = append(helper, 0x20, 0, 0x0b)
			code := append(wasmtest.ULEB(uint32(len(body))), body...)
			helperCode := append(wasmtest.ULEB(uint32(len(helper))), helper...)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "addr", wasm.I32, true))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
				wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
				wasmtest.Section(10, wasmtest.Vec(code, helperCode)),
			)
			for _, mode := range modes {
				for _, enabled := range []bool{false, true} {
					t.Run(fmt.Sprintf("locals_%d/%s/%v/facts_%v", count, shape, mode, enabled), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithBoundsChecks(mode).WithOptimization("value-facts", enabled)
						compiled, err := Compile(cfg, data)
						if err != nil {
							t.Fatal(err)
						}
						defer compiled.Close()
						global := NewGlobalI32(0, true)
						defer global.Close()
						in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.addr": GlobalImport{Global: global}}})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						binary.LittleEndian.PutUint32(in.Memory().UnsafeBytes(), 42)
						for _, choice := range []uint64{0, 1} {
							got, err := in.Invoke("load", uint64(1)<<32, choice)
							if err != nil || len(got) != 1 || got[0] != 42 {
								t.Fatalf("load = %v, %v; want 42", got, err)
							}
						}
						if err := global.Set(I32(65535)); err != nil {
							t.Fatal(err)
						}
						for _, choice := range []uint64{0, 1} {
							if _, err := in.Invoke("load", I32(65535), choice); err == nil {
								t.Fatal("out-of-bounds load did not trap")
							}
						}
					})
				}
			}
		}
	}
}
