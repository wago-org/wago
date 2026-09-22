//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMemoryBulkPreservesLiveV128Slots(t *testing.T) {
	const wantResult = uint64(0x123456789abcdef0)
	data := make([]byte, 8192)
	for i := range data {
		data[i] = byte(i*17 + 3)
	}
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, op := range []string{"fill", "init"} {
		for _, mode := range modes {
			t.Run(fmt.Sprintf("%s/bounds=%d", op, mode), func(t *testing.T) {
				body := []byte{0xfd, 0x0c}
				body = binary.LittleEndian.AppendUint64(body, wantResult)
				body = binary.LittleEndian.AppendUint64(body, 0xfedcba9876543210)
				body = append(body, 0x20, 0, 0x20, 1, 0x20, 2)
				if op == "fill" {
					body = append(body, 0xfc, 0x0b, 0)
				} else {
					body = append(body, 0xfc, 0x08, 0, 0)
				}
				body = append(body, 0xfd, 0x1d, 0, 0x0b)
				segment := append([]byte{1}, wasmtest.ULEB(uint32(len(data)))...)
				segment = append(segment, data...)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
						[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
					wasmtest.Section(7, wasmtest.Vec(
						wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
					wasmtest.Section(12, wasmtest.ULEB(1)),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					wasmtest.Section(11, wasmtest.Vec(segment)),
				)
				compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(mode), module)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				mem := instance.Memory().UnsafeBytes()
				for _, n := range []int{0, 1, 15, 16, 31, 63, 64, 255, 256, 257, 1023, 1024, 1025} {
					for _, dst := range []int{33, len(mem) - n} {
						t.Run(fmt.Sprintf("n=%d/dst=%d", n, dst), func(t *testing.T) {
							for i := range mem {
								mem[i] = 0xbb
							}
							want := append([]byte(nil), mem...)
							arg := 8
							if op == "fill" {
								arg = 0x1a7
								for i := dst; i < dst+n; i++ {
									want[i] = 0xa7
								}
							} else {
								copy(want[dst:dst+n], data[arg:arg+n])
							}
							got, err := instance.Invoke("run", I32(int32(dst)), I32(int32(arg)), I32(int32(n)))
							if err != nil || len(got) != 1 || got[0] != wantResult {
								t.Fatalf("result = %x, %v; want %x", got, err, wantResult)
							}
							if !bytes.Equal(mem, want) {
								t.Error("bulk operation changed the wrong memory bytes")
							}
						})
					}
				}
				invalid := [][3]int{{len(mem), 0, 1}, {len(mem) + 1, 0, 0}}
				if op == "init" {
					invalid = append(invalid, [3]int{0, len(data), 1})
				}
				for _, args := range invalid {
					before := append([]byte(nil), mem...)
					if _, err := instance.Invoke("run", I32(int32(args[0])), I32(int32(args[1])), I32(int32(args[2]))); err == nil {
						t.Errorf("arguments %v did not trap", args)
					}
					if !bytes.Equal(mem, before) {
						t.Errorf("arguments %v changed memory before trapping", args)
					}
				}
			})
		}
	}
}
