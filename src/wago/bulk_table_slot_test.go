//go:build amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestTableBulkPreservesLiveV128Slots(t *testing.T) {
	const want = uint64(0x123456789abcdef0)
	for _, op := range []string{"init", "copy", "fill", "externref-fill"} {
		t.Run(op, func(t *testing.T) {
			body := []byte{0xfd, 0x0c}
			body = binary.LittleEndian.AppendUint64(body, want)
			body = binary.LittleEndian.AppendUint64(body, 0xfedcba9876543210)
			body = append(body, 0x20, 0)
			if op == "externref-fill" {
				body = append(body, 0xd0, 0x6f)
			} else if op == "fill" {
				body = append(body, 0xd2, 1)
			} else {
				body = append(body, 0x20, 1)
			}
			body = append(body, 0x20, 2)
			switch op {
			case "init":
				body = append(body, 0xfc, 0x0c, 1, 0)
			case "copy":
				body = append(body, 0xfc, 0x0e, 0, 0)
			case "fill", "externref-fill":
				body = append(body, 0xfc, 0x11, 0)
			}
			body = append(body, 0xfd, 0x1d, 0, 0x0b)
			tableType := byte(0x70)
			segments := [][]byte{
				{0, 0x41, 0, 0x0b, 1, 1},
				{1, 0, 8, 1, 1, 1, 1, 1, 1, 1, 1},
			}
			if op == "externref-fill" {
				tableType = 0x6f
				segments = segments[1:]
			}
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}),
					wasmtest.FuncType(nil, nil),
					wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
				wasmtest.Section(4, wasmtest.Vec([]byte{tableType, 0, 8})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("is_null", 0, 2))),
				wasmtest.Section(9, wasmtest.Vec(segments...)),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0xd1, 0x0b}))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			for _, n := range []int{0, 1, 4} {
				t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
					instance, err := Instantiate(compiled, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close()
					got, err := instance.Invoke("run", I32(1), I32(0), I32(int32(n)))
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("result = %x, %v; want %x", got, err, want)
					}
					for i := 0; i < 8; i++ {
						wantNull := uint64(1)
						if op != "externref-fill" {
							if i == 0 || (op == "copy" && n > 0 && i == 1) || (op != "copy" && i >= 1 && i < 1+n) {
								wantNull = 0
							}
						}
						entry, err := instance.Invoke("is_null", I32(int32(i)))
						if err != nil || len(entry) != 1 || entry[0] != wantNull {
							t.Fatalf("entry %d null = %v, %v; want %d", i, entry, err, wantNull)
						}
					}
				})
			}
		})
	}
}
