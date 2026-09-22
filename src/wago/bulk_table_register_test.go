//go:build amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestTableFillPreservesCachedValues(t *testing.T) {
	for _, operation := range []string{"fill", "grow"} {
		for _, family := range []string{"funcref", "externref"} {
			for _, kind := range []string{"f32", "f64", "v128"} {
				t.Run(operation+"/"+family+"/"+kind, func(t *testing.T) {
					params := []wasm.ValType{wasm.I32}
					result := wasm.I64
					var extra []uint64
					var want uint64
					var body []byte
					if operation == "fill" {
						body = append(body, 0x41, 0)
					}
					tableType := byte(0x70)
					if family == "externref" {
						tableType = 0x6f
						body = append(body, 0xd0, 0x6f)
					} else {
						body = append(body, 0xd2, 1)
					}
					if operation == "fill" {
						body = append(body, 0x20, 0, 0xfc, 0x11, 0)
					} else {
						body = append(body, 0x20, 0, 0xfc, 0x0f, 0)
					}
					switch kind {
					case "f32":
						params = append(params, wasm.F32)
						result = wasm.F32
						extra = []uint64{F32(2)}
						want = F32(200)
						body = append(body, 0x20, 1, 0x43)
						body = binary.LittleEndian.AppendUint32(body, math.Float32bits(100))
						body = append(body, 0x94)
					case "f64":
						params = append(params, wasm.F64)
						result = wasm.F64
						extra = []uint64{F64(2)}
						want = F64(200)
						body = append(body, 0x20, 1, 0x44)
						body = binary.LittleEndian.AppendUint64(body, math.Float64bits(100))
						body = append(body, 0xa2)
					case "v128":
						want = 0x123456789abcdef0
						body = append(body, 0xfd, 0x0c)
						body = binary.LittleEndian.AppendUint64(body, want)
						body = binary.LittleEndian.AppendUint64(body, 0xfedcba9876543210)
						body = append(body, 0xfd, 0x1d, 0)
					}
					body = append(body, 0x0b)
					results := []wasm.ValType{result}
					tableDecl := []byte{tableType, 1, 32, 32}
					if operation == "grow" {
						results = []wasm.ValType{wasm.I32, result}
						tableDecl = []byte{tableType, 1, 4, 32}
					}
					module := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(
							wasmtest.FuncType(params, results),
							wasmtest.FuncType(nil, nil),
							wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
						wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
						wasmtest.Section(4, wasmtest.Vec(tableDecl)),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("is_null", 0, 2), wasmtest.ExportEntry("size", 0, 3))),
						wasmtest.Section(9, wasmtest.Vec([]byte{1, 0, 1, 1})),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0xd1, 0x0b}), wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}))),
					)
					compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					counts := []int{0, 7, 8, 9, 16}
					if operation == "grow" {
						counts = append(counts, 29)
					}
					for _, n := range counts {
						t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
							instance, err := Instantiate(compiled, InstantiateOptions{})
							if err != nil {
								t.Fatal(err)
							}
							defer instance.Close()
							args := append([]uint64{I32(int32(n))}, extra...)
							got, err := instance.Invoke("run", args...)
							wantValues := []uint64{want}
							tableSize, first, last := 32, 0, n
							if operation == "grow" {
								old := I32(4)
								tableSize, first, last = 4+n, 4, 4+n
								if n > 28 {
									old = I32(-1)
									tableSize, first, last = 4, 0, 0
								}
								wantValues = []uint64{old, want}
							}
							if err != nil || !slices.Equal(got, wantValues) {
								t.Errorf("result = %x, %v; want %x", got, err, wantValues)
							}
							size, err := instance.Invoke("size")
							if err != nil || len(size) != 1 || size[0] != uint64(tableSize) {
								t.Fatalf("size = %v, %v; want %d", size, err, tableSize)
							}
							for i := 0; i < tableSize; i++ {
								wantNull := uint64(1)
								if family == "funcref" && i >= first && i < last {
									wantNull = 0
								}
								got, err := instance.Invoke("is_null", I32(int32(i)))
								if err != nil || len(got) != 1 || got[0] != wantNull {
									t.Fatalf("entry %d null = %v, %v; want %d", i, got, err, wantNull)
								}
							}
						})
					}
				})
			}
		}
	}

}

func TestTableGrowPreservesFloatLocalsOnFailure(t *testing.T) {
	params := []wasm.ValType{wasm.I32}
	args := []uint64{0}
	for i := 0; i < 12; i++ {
		params = append(params, wasm.F64)
		args = append(args, F64(2))
	}
	var body []byte
	for i := uint64(1); i <= 4; i++ {
		body = append(body, 0xfd, 0x0c)
		body = binary.LittleEndian.AppendUint64(body, 0x123456789abc0000+i)
		body = binary.LittleEndian.AppendUint64(body, 0xfedcba9876540000+i)
		body = append(body, 0x1a)
	}
	body = append(body, 0xd2, 1, 0x20, 0, 0xfc, 0x0f, 0)
	for i := 1; i <= 12; i++ {
		body = append(body, 0x20, byte(i), 0x20, byte(i), 0xa0)
		if i > 1 {
			body = append(body, 0xa0)
		}
	}
	body = append(body, 0x44)
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(100))
	body = append(body, 0xa2, 0x44)
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(37))
	body = append(body, 0xa0, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(params, []wasm.ValType{wasm.I32, wasm.F64}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 4, 32})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("size", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec([]byte{1, 0, 1, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, n := range []int{0, 8, 29} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			args[0] = I32(int32(n))
			old, size := I32(4), uint64(4+n)
			if n > 28 {
				old, size = I32(-1), 4
			}
			got, err := instance.Invoke("run", args...)
			want := []uint64{old, F64(4837)}
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("result = %x, %v; want %x", got, err, want)
			}
			got, err = instance.Invoke("size")
			if err != nil || len(got) != 1 || got[0] != size {
				t.Fatalf("size = %v, %v; want %d", got, err, size)
			}
		})
	}
}
