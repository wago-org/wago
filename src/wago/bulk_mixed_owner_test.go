//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBulkMemoryMixedStackOwners(t *testing.T) {
	const loA, hiA = uint64(0x1122334455667788), uint64(0x8877665544332211)
	const loB, hiB = uint64(0x123456789abcdef0), uint64(0xfedcba9876543210)
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i*19 + 5)
	}
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, tc := range []struct {
		op       string
		constant int
	}{
		{"copy", -1}, {"fill", -1}, {"init", -1}, {"copy", 8}, {"copy", 33}, {"copy", 64},
	} {
		for _, mode := range modes {
			t.Run(fmt.Sprintf("%s/constant=%d/bounds=%d", tc.op, tc.constant, mode), func(t *testing.T) {
				body := []byte{0x20, 3, 0xfd, 0x0c}
				body = binary.LittleEndian.AppendUint64(body, loA)
				body = binary.LittleEndian.AppendUint64(body, hiA)
				body = append(body, 0x41, 0x6f, 0x44) // i32.const -17; f64.const -2.5
				body = binary.LittleEndian.AppendUint64(body, F64(-2.5))
				body = append(body, 0xfd, 0x0c)
				body = binary.LittleEndian.AppendUint64(body, loB)
				body = binary.LittleEndian.AppendUint64(body, hiB)
				body = append(body, 0x42, 0x77, 0x20, 4, 0x20, 0, 0x20, 1) // i64.const -9; ref; dst; arg
				if tc.constant < 0 {
					body = append(body, 0x20, 2)
				} else {
					body = append(body, 0x41)
					body = append(body, wasmtest.SLEB32(int32(tc.constant))...)
				}
				switch tc.op {
				case "copy":
					body = append(body, 0xfc, 0x0a, 0, 0)
				case "fill":
					body = append(body, 0xfc, 0x0b, 0)
				case "init":
					body = append(body, 0xfc, 0x08, 0, 0)
				}
				body = append(body, 0x0b)
				segment := append([]byte{1}, wasmtest.ULEB(uint32(len(data)))...)
				segment = append(segment, data...)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(
						wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.ExternRef, wasm.FuncRef}, []wasm.ValType{wasm.ExternRef, wasm.V128, wasm.I32, wasm.F64, wasm.V128, wasm.I64, wasm.FuncRef}),
						wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
					wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("getref", 0, 2), wasmtest.ExportEntry("memory", 2, 0))),
					wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 1})),
					wasmtest.Section(12, wasmtest.ULEB(1)),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 17, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x0b}))),
					wasmtest.Section(11, wasmtest.Vec(segment)),
				)
				rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(mode)))
				defer rt.Close()
				mod, err := rt.Compile(module)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				in, err := rt.Instantiate(context.Background(), mod)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				payload := &struct{ name string }{"this instance"}
				ext, err := rt.NewExternRef(payload)
				if err != nil {
					t.Fatal(err)
				}
				fun, err := in.Invoke("getref")
				if err != nil || len(fun) != 1 {
					t.Fatalf("getref = %v, %v", fun, err)
				}
				funcToken := fun[0]
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				wantSlots := []uint64{ext.token, loA, hiA, I32(-17), F64(-2.5), loB, hiB, I64(-9), funcToken}
				mem := in.Memory().UnsafeBytes()
				counts := []int{0, 8, 31, 64, 255, 256, 1024}
				if tc.constant >= 0 {
					counts = []int{tc.constant}
				}
				for _, n := range counts {
					t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
						for i := range mem {
							mem[i] = byte(i*31 + 7)
						}
						wantMem := append([]byte(nil), mem...)
						dst, arg := 33, 32
						switch tc.op {
						case "copy":
							copy(wantMem[dst:dst+n], wantMem[arg:arg+n])
						case "fill":
							arg = 0x1a7
							for i := dst; i < dst+n; i++ {
								wantMem[i] = 0xa7
							}
						case "init":
							arg = 3
							copy(wantMem[dst:dst+n], data[arg:arg+n])
						}
						got, err := fn.Invoke(I32(int32(dst)), I32(int32(arg)), I32(int32(n)), ext.token, funcToken)
						if err != nil || !slices.Equal(got, wantSlots) {
							t.Fatalf("result slots = %x, %v; want %x", got, err, wantSlots)
						}
						value, ok := in.ExternRefValue(ValueOf(ValExternRef, got[0]).ExternRef())
						if !ok || value != payload {
							t.Fatalf("externref resolved to %v, %v; want original identity", value, ok)
						}
						if !bytes.Equal(mem, wantMem) {
							t.Fatal("bulk operation changed wrong memory bytes")
						}
					})
				}
			})
		}
	}
}

func TestDeferredEventAboveLiveV128(t *testing.T) {
	body := []byte{0xfd, 0x0c}
	body = binary.LittleEndian.AppendUint64(body, 0x123456789abcdef0)
	body = binary.LittleEndian.AppendUint64(body, 0x1122334455667788)
	body = append(body, 0x41, 42, 0x10, 0, 0xfd, 0x1d, 0, 0x0b)
	imp := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	imp = append(imp, 0, 0)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var events []int32
	imports := NewImports()
	imports.I32Event("env", "event", func(v int32) { events = append(events, v) })
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 0x123456789abcdef0 {
		t.Fatalf("result = %x, %v", got, err)
	}
	if !slices.Equal(events, []int32{42}) {
		t.Fatalf("events = %v, want [42]", events)
	}
}

func TestTableBulkMixedStackReferenceIdentity(t *testing.T) {
	for _, grow := range []bool{false, true} {
		name := "fill"
		if grow {
			name = "grow"
		}
		t.Run(name, func(t *testing.T) { runTableMixedStackReferenceIdentity(t, grow) })
	}
}

func runTableMixedStackReferenceIdentity(t *testing.T, grow bool) {
	const loA, hiA = uint64(0x1122334455667788), uint64(0x8877665544332211)
	const loB, hiB = uint64(0x123456789abcdef0), uint64(0xfedcba9876543210)
	body := []byte{0x20, 1, 0xfd, 0x0c}
	body = binary.LittleEndian.AppendUint64(body, loA)
	body = binary.LittleEndian.AppendUint64(body, hiA)
	body = append(body, 0x41, 0x6f, 0xfd, 0x0c)
	body = binary.LittleEndian.AppendUint64(body, loB)
	body = binary.LittleEndian.AppendUint64(body, hiB)
	body = append(body, 0x20, 2)
	results := []wasm.ValType{wasm.ExternRef, wasm.V128, wasm.I32, wasm.V128, wasm.FuncRef}
	tables := [][]byte{{0x70, 0, 32}, {0x6f, 0, 32}}
	if grow {
		body = append(body, 0x20, 2, 0x20, 0, 0xfc, 0x0f, 0, 0x20, 1, 0x20, 0, 0xfc, 0x0f, 1, 0x0b)
		results = append(results, wasm.I32, wasm.I32)
		tables = [][]byte{{0x70, 1, 0, 32}, {0x6f, 1, 0, 32}}
	} else {
		body = append(body, 0x41, 0, 0x20, 2, 0x20, 0, 0xfc, 0x11, 0, 0x41, 0, 0x20, 1, 0x20, 0, 0xfc, 0x11, 1, 0x0b)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.ExternRef, wasm.FuncRef}, results),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.ExternRef}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(4, wasmtest.Vec(tables...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("get_func", 0, 2), wasmtest.ExportEntry("get_ext", 0, 3), wasmtest.ExportEntry("seed", 0, 4))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x25, 1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x0b}))),
	)
	rt := NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	payload := &struct{ name string }{"table owner"}
	ext, err := rt.NewExternRef(payload)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := in.Invoke("seed")
	if err != nil || len(seed) != 1 {
		t.Fatalf("seed = %v, %v", seed, err)
	}
	funcToken := seed[0]
	want := []uint64{ext.token, loA, hiA, I32(-17), loB, hiB, funcToken}
	size := 0
	for _, n := range []int{0, 8, 9, 16} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			expected := want
			entries, filled := 32, n
			if grow {
				old := I32(int32(size))
				if size+n > 32 {
					old = I32(-1)
				} else {
					size += n
				}
				expected = append(append([]uint64(nil), want...), old, old)
				entries, filled = size, size
			}
			got, err := in.Invoke("run", I32(int32(n)), ext.token, funcToken)
			if err != nil || !slices.Equal(got, expected) {
				t.Fatalf("result slots = %x, %v; want %x", got, err, expected)
			}
			for i := 0; i < entries; i++ {
				wantFunc, wantExt := uint64(0), uint64(0)
				if i < filled {
					wantFunc, wantExt = funcToken, ext.token
				}
				fun, err := in.Invoke("get_func", I32(int32(i)))
				if err != nil || len(fun) != 1 || fun[0] != wantFunc {
					t.Fatalf("funcref[%d] = %x, %v; want %#x", i, fun, err, wantFunc)
				}
				ref, err := in.Invoke("get_ext", I32(int32(i)))
				if err != nil || len(ref) != 1 || ref[0] != wantExt {
					t.Fatalf("externref[%d] = %x, %v; want %#x", i, ref, err, wantExt)
				}
				if i < filled {
					value, ok := in.ExternRefValue(ValueOf(ValExternRef, ref[0]).ExternRef())
					if !ok || value != payload {
						t.Fatalf("externref[%d] lost its owner identity", i)
					}
				}
			}
		})
	}
}
