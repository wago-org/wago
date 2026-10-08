//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type rv64MMFunc struct {
	name            string
	params, results []wasm.ValType
	body            []byte
}

// Every fixture supplies explicit maxima, including zero-capacity memories.
func rv64MMModule(funcs []rv64MMFunc, defs [][]byte, imports [][]byte, count int, data [][]byte, swar ...bool) []byte {
	var types, functions, exports, bodies [][]byte
	for i, f := range funcs {
		types = append(types, wasmtest.FuncType(f.params, f.results))
		functions = append(functions, wasmtest.ULEB(uint32(i)))
		exports = append(exports, wasmtest.ExportEntry(f.name, 0, uint32(i)))
		bodies = append(bodies, wasmtest.Code(f.body))
	}
	if len(swar) != 0 && swar[0] {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	for i := 0; i < count; i++ {
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("m%d", i), 2, uint32(i)))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(functions...)))
	if len(defs) != 0 {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(defs...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if len(data) != 0 {
		sections = append(sections, wasmtest.Section(12, wasmtest.ULEB(uint32(len(data)))))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(bodies...)))
	if len(data) != 0 {
		sections = append(sections, wasmtest.Section(11, wasmtest.Vec(data...)))
	}
	return wasmtest.Module(sections...)
}
func rv64MMImport(name string, min, max byte) []byte {
	return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 2, 1, min, max)
}
func rv64MMArg(op []byte, index uint32, offset uint32) []byte {
	out := append(slices.Clone(op), 0x40) // Explicit indexed memarg, conservative alignment.
	out = append(out, wasmtest.ULEB(index)...)
	return append(out, wasmtest.ULEB(offset)...)
}
func rv64MMCall(t *testing.T, in *Instance, name string, want []uint64, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("%s(%x): got %x / %v, want %x", name, args, got, err, want)
	}
}
func rv64MMTrap(t *testing.T, in *Instance, name string, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	var trap *coreruntime.TrapError
	if got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds {
		t.Fatalf("%s(%x): want exact memory trap/no results, got %x / %v", name, args, got, err)
	}
}
func rv64MMInstance(t *testing.T, c *Compiled, opts ...any) *Instance {
	t.Helper()
	in, err := Instantiate(c, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}
func rv64MMBytes(t *testing.T, in *Instance, index int) []byte {
	t.Helper()
	m, err := in.ExportedMemory(fmt.Sprintf("m%d", index))
	if err != nil {
		t.Fatal(err)
	}
	return m.UnsafeBytes()
}

func TestRV64PublicMultiMemoryScalarAndFP(t *testing.T) {
	cases := []struct {
		name        string
		typ         wasm.ValType
		load, store byte
		width       int
		input, want uint64
	}{
		{"i32", wasm.I32, 0x28, 0x36, 4, 0xdeadbeef80000001, 0x80000001},
		{"i64", wasm.I64, 0x29, 0x37, 8, 0x80000000deadbeef, 0x80000000deadbeef},
		{"f32", wasm.F32, 0x2a, 0x38, 4, 0x7fa00001, 0x7fa00001},
		{"f64", wasm.F64, 0x2b, 0x39, 8, 0x7ff0000000000001, 0x7ff0000000000001},
		{"i32_8s", wasm.I32, 0x2c, 0x3a, 1, 0x80, 0xffffff80}, {"i32_8u", wasm.I32, 0x2d, 0x3a, 1, 0x80, 0x80},
		{"i32_16s", wasm.I32, 0x2e, 0x3b, 2, 0x8001, 0xffff8001}, {"i32_16u", wasm.I32, 0x2f, 0x3b, 2, 0x8001, 0x8001},
		{"i64_8s", wasm.I64, 0x30, 0x3c, 1, 0x80, 0xffffffffffffff80}, {"i64_8u", wasm.I64, 0x31, 0x3c, 1, 0x80, 0x80},
		{"i64_16s", wasm.I64, 0x32, 0x3d, 2, 0x8001, 0xffffffffffff8001}, {"i64_16u", wasm.I64, 0x33, 0x3d, 2, 0x8001, 0x8001},
		{"i64_32s", wasm.I64, 0x34, 0x3e, 4, 0x80000001, 0xffffffff80000001}, {"i64_32u", wasm.I64, 0x35, 0x3e, 4, 0x80000001, 0x80000001},
	}
	for _, swar := range []bool{false, true} {
		var funcs []rv64MMFunc
		for m := 0; m < 2; m++ {
			for _, tc := range cases {
				name := fmt.Sprintf("m%d_%s", m, tc.name)
				load := append([]byte{0x20, 0}, rv64MMArg([]byte{tc.load}, uint32(m), 0)...)
				store := append([]byte{0x20, 0, 0x20, 1}, rv64MMArg([]byte{tc.store}, uint32(m), 0)...)
				funcs = append(funcs, rv64MMFunc{name + "_load", []wasm.ValType{wasm.I32}, []wasm.ValType{tc.typ}, append(load, 0x0b)}, rv64MMFunc{name + "_store", []wasm.ValType{wasm.I32, tc.typ}, nil, append(store, 0x0b)})
			}
		}
		overflow := append([]byte{0x20, 0}, rv64MMArg([]byte{0x28}, 1, ^uint32(0))...)
		funcs = append(funcs, rv64MMFunc{"offset_overflow", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, append(overflow, 0x0b)})
		if swar {
			funcs = append(funcs, rv64MMFunc{"vector", []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, []byte{0x20, 0, 0x0b}})
		}
		source := rv64MMModule(funcs, [][]byte{{1, 1, 1}, {1, 1, 1}}, nil, 2, nil)
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar%t-artifact%t", swar, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("multimemory-scalar-swar%t", swar), source, artifact)
				in := rv64MMInstance(t, c)
				memory := [][]byte{rv64MMBytes(t, in, 0), rv64MMBytes(t, in, 1)}
				for m := 0; m < 2; m++ {
					for _, tc := range cases {
						name := fmt.Sprintf("m%d_%s", m, tc.name)
						for _, address := range []int{3, 65536 - tc.width} {
							other := slices.Clone(memory[1-m][address : address+tc.width])
							rv64MMCall(t, in, name+"_store", nil, 0xdeadbeef00000000|uint64(address), tc.input)
							rv64MMCall(t, in, name+"_load", []uint64{tc.want}, 0xaabbccdd00000000|uint64(address))
							if !bytes.Equal(other, memory[1-m][address:address+tc.width]) {
								t.Fatal("store selected wrong memory", name)
							}
						}
						tail := slices.Clone(memory[m][65520:])
						bad := uint64(65537 - tc.width)
						rv64MMTrap(t, in, name+"_load", bad)
						rv64MMTrap(t, in, name+"_store", bad, tc.input)
						if !bytes.Equal(tail, memory[m][65520:]) {
							t.Fatal("partial out-of-bounds store", name)
						}
					}
				}
				rv64MMTrap(t, in, "offset_overflow", 1)
			})
		}
	}
}

func rv64MMBulkFuncs() []rv64MMFunc {
	var funcs []rv64MMFunc
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	for dst := byte(0); dst < 2; dst++ {
		for src := byte(0); src < 2; src++ {
			funcs = append(funcs, rv64MMFunc{fmt.Sprintf("copy%d%d", dst, src), params, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, dst, src, 0x0b}})
		}
		funcs = append(funcs, rv64MMFunc{fmt.Sprintf("fill%d", dst), params, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, dst, 0x0b}}, rv64MMFunc{fmt.Sprintf("init%d", dst), params, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 8, 1, dst, 0x0b}})
	}
	return append(funcs, rv64MMFunc{"drop", nil, nil, []byte{0xfc, 9, 1, 0x0b}})
}
func TestRV64PublicMultiMemoryBulkAndAliases(t *testing.T) {
	imports := [][]byte{rv64MMImport("a", 1, 2), rv64MMImport("b", 1, 2)}
	data := [][]byte{{2, 1, 0x41, 0, 0x0b, 2, 42, 43}, {1, 4, 0x80, 0x81, 0xfe, 0xff}}
	for _, swar := range []bool{false, true} {
		source := rv64MMModule(rv64MMBulkFuncs(), nil, imports, 2, data, swar)
		for _, artifact := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				t.Run(fmt.Sprintf("swar%t-artifact%t-alias%t", swar, artifact, alias), func(t *testing.T) {
					c := rv64TableStageCompile(t, fmt.Sprintf("multimemory-bulk-swar%t", swar), source, artifact)
					a, err := NewSharedMemory(1, 2)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = a.Close() })
					b := a
					if !alias {
						b, err = NewSharedMemory(1, 2)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = b.Close() })
					}
					in := rv64MMInstance(t, c, NewImports().Memory("env", "a", a).Memory("env", "b", b))
					memory := [][]byte{a.UnsafeBytes(), b.UnsafeBytes()}
					if !bytes.Equal(memory[1][:2], []byte{42, 43}) {
						t.Fatal("secondary active data lost")
					}
					for dst := 0; dst < 2; dst++ {
						for src := 0; src < 2; src++ {
							for _, pair := range [][2]int{{4, 0}, {0, 4}, {0, 0}} {
								for i := 0; i < 64; i++ {
									memory[0][i] = byte(i)
									memory[1][i] = byte(i + 64)
								}
								expected := slices.Clone(memory[dst][:64])
								input := slices.Clone(memory[src][pair[1] : pair[1]+24])
								copy(expected[pair[0]:], input)
								rv64MMCall(t, in, fmt.Sprintf("copy%d%d", dst, src), nil, 0xdeadbeef00000000|uint64(pair[0]), 0xaabbccdd00000000|uint64(pair[1]), 0x9999999900000018)
								if !bytes.Equal(expected, memory[dst][:64]) {
									t.Fatal("indexed overlap copy", dst, src, pair)
								}
								before := slices.Clone(memory[dst][:64])
								rv64MMTrap(t, in, fmt.Sprintf("copy%d%d", dst, src), 0, 65535, 2)
								if !bytes.Equal(before, memory[dst][:64]) {
									t.Fatal("source trap partially copied")
								}
								rv64MMCall(t, in, fmt.Sprintf("copy%d%d", dst, src), nil, 65536, 65536, 0)
								rv64MMTrap(t, in, fmt.Sprintf("copy%d%d", dst, src), 65537, 65536, 0)
								rv64MMTrap(t, in, fmt.Sprintf("copy%d%d", dst, src), 65536, 65537, 0)
							}
						}
					}
					for m := 0; m < 2; m++ {
						rv64MMCall(t, in, fmt.Sprintf("fill%d", m), nil, 3, 0xdeadbeef12345680, 7)
						if !bytes.Equal(memory[m][3:10], bytes.Repeat([]byte{0x80}, 7)) {
							t.Fatal("fill value truncation")
						}
						rv64MMCall(t, in, fmt.Sprintf("init%d", m), nil, 20, 0, 4)
						if !bytes.Equal(memory[m][20:24], data[1][2:]) {
							t.Fatal("passive original segment index")
						}
						tail := slices.Clone(memory[m][65520:])
						rv64MMTrap(t, in, fmt.Sprintf("fill%d", m), 65535, 9, 2)
						rv64MMTrap(t, in, fmt.Sprintf("init%d", m), 65535, 0, 2)
						if !bytes.Equal(tail, memory[m][65520:]) {
							t.Fatal("bulk trap partially wrote")
						}
					}
					rv64MMCall(t, in, "drop", nil)
					for m := 0; m < 2; m++ {
						rv64MMCall(t, in, fmt.Sprintf("init%d", m), nil, 65536, 0, 0)
						rv64MMTrap(t, in, fmt.Sprintf("init%d", m), 0, 1, 0)
						rv64MMTrap(t, in, fmt.Sprintf("init%d", m), 0, 0, 1)
					}
				})
			}
		}
	}
}

func TestRV64PublicMultiMemorySWAR(t *testing.T) {
	type vectorCase struct {
		name  string
		sub   byte
		width int
		lane  int
		store bool
	}
	var cases []vectorCase
	for sub := byte(0); sub <= 11; sub++ {
		width := 16
		switch {
		case sub >= 1 && sub <= 6:
			width = 8
		case sub >= 7 && sub <= 10:
			width = 1 << uint(sub-7)
		}
		cases = append(cases, vectorCase{fmt.Sprintf("op%d", sub), sub, width, -1, sub == 11})
	}
	cases = append(cases, vectorCase{"zero32", 92, 4, -1, false}, vectorCase{"zero64", 93, 8, -1, false})
	for sub := byte(84); sub <= 91; sub++ {
		width := 1 << uint((sub-84)%4)
		for lane := 0; lane < 16/width; lane++ {
			cases = append(cases, vectorCase{fmt.Sprintf("op%d_lane%d", sub, lane), sub, width, lane, sub >= 88})
		}
	}
	var funcs []rv64MMFunc
	for _, tc := range cases {
		params := []wasm.ValType{wasm.I32}
		results := []wasm.ValType{wasm.V128}
		body := []byte{0x20, 0}
		if tc.lane >= 0 || tc.store {
			params = append(params, wasm.V128)
			body = append(body, 0x20, 1)
		}
		body = append(body, rv64MMArg([]byte{0xfd, tc.sub}, 1, 0)...)
		if tc.lane >= 0 {
			body = append(body, byte(tc.lane))
		}
		body = append(body, 0x0b)
		if tc.store {
			results = nil
		}
		funcs = append(funcs, rv64MMFunc{tc.name, params, results, body})
	}
	source := rv64MMModule(funcs, [][]byte{{1, 1, 1}, {1, 1, 1}}, nil, 2, nil)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "multimemory-swar", source, artifact)
			in := rv64MMInstance(t, c)
			memory := rv64MMBytes(t, in, 1)
			other := rv64MMBytes(t, in, 0)
			seed := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
			input := [16]byte{0x80, 0xfe, 0x7f, 0xff, 0x81, 0x80, 0, 0xff, 9, 10, 11, 12, 13, 14, 15, 16}
			for _, tc := range cases {
				for _, address := range []int{3, 65536 - tc.width} {
					copy(memory[address:], input[:tc.width])
					beforeOther := slices.Clone(other[address : address+tc.width])
					expected := make([]byte, 16)
					args := []uint64{0xdeadbeef00000000 | uint64(address)}
					if tc.lane >= 0 || tc.store {
						args = append(args, binary.LittleEndian.Uint64(seed[:8]), binary.LittleEndian.Uint64(seed[8:]))
					}
					if tc.store {
						rv64MMCall(t, in, tc.name, nil, args...)
						start := 0
						if tc.lane >= 0 {
							start = tc.lane * tc.width
						}
						if !bytes.Equal(memory[address:address+tc.width], seed[start:start+tc.width]) {
							t.Fatal("vector store/lane", tc.name)
						}
					} else {
						switch {
						case tc.lane >= 0:
							copy(expected, seed[:])
							copy(expected[tc.lane*tc.width:], input[:tc.width])
						case tc.sub >= 1 && tc.sub <= 6:
							narrow := 1 << uint((tc.sub-1)/2)
							signed := tc.sub%2 == 1
							for lane := 0; lane < 8/narrow; lane++ {
								var value uint64
								for b := 0; b < narrow; b++ {
									value |= uint64(input[lane*narrow+b]) << uint(8*b)
								}
								if signed && value&(uint64(1)<<uint(8*narrow-1)) != 0 {
									value |= ^uint64(0) << uint(8*narrow)
								}
								for b := 0; b < 2*narrow; b++ {
									expected[lane*2*narrow+b] = byte(value >> uint(8*b))
								}
							}
						case tc.sub >= 7 && tc.sub <= 10:
							for start := 0; start < 16; start += tc.width {
								copy(expected[start:], input[:tc.width])
							}
						default:
							copy(expected, input[:tc.width])
						}
						rv64MMCall(t, in, tc.name, []uint64{binary.LittleEndian.Uint64(expected[:8]), binary.LittleEndian.Uint64(expected[8:])}, args...)
					}
					if !bytes.Equal(beforeOther, other[address:address+tc.width]) {
						t.Fatal("vector touched memory zero", tc.name)
					}
					tail := slices.Clone(memory[65520:])
					args[0] = uint64(65537 - tc.width)
					rv64MMTrap(t, in, tc.name, args...)
					if !bytes.Equal(tail, memory[65520:]) {
						t.Fatal("vector trap partially wrote", tc.name)
					}
				}
			}
		})
	}
}

func rv64MMGrowFuncs(index byte) []rv64MMFunc {
	load := append([]byte{0x20, 0}, rv64MMArg([]byte{0x28}, uint32(index), 0)...)
	store := append([]byte{0x20, 0, 0x20, 1}, rv64MMArg([]byte{0x36}, uint32(index), 0)...)
	return []rv64MMFunc{
		{"size", nil, []wasm.ValType{wasm.I32}, []byte{0x3f, index, 0x0b}},
		{"grow", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x40, index, 0x0b}},
		{"grow0_size", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x20, 0, 0x40, 0, 0x3f, index, 0x0b}},
		{"load", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, append(load, 0x0b)},
		{"store", []wasm.ValType{wasm.I32, wasm.I32}, nil, append(store, 0x0b)},
		// Access the newly-grown bytes before returning through any bridge refresh.
		{"grow0_store", []wasm.ValType{wasm.I32, wasm.I32}, nil, append([]byte{0x41, 1, 0x40, 0, 0x1a}, append(store, 0x0b)...)},
	}
}
func TestRV64PublicMultiMemoryGrowthAndHighIndex(t *testing.T) {
	for _, count := range []int{2, 100} {
		for _, alias := range []bool{false, true} {
			index := count - 1
			defs := make([][]byte, count)
			var imports [][]byte
			for i := range defs {
				defs[i] = []byte{1, 0, 0}
			}
			defs[0] = []byte{1, 1, 3}
			defs[index] = []byte{1, 1, 3}
			if alias {
				defs = nil
				for i := 0; i < count; i++ {
					name := "zero"
					min, max := byte(0), byte(0)
					if i == 0 || i == index {
						name = "shared"
						min, max = 1, 3
					}
					imports = append(imports, rv64MMImport(name, min, max))
				}
			}
			funcs := rv64MMGrowFuncs(byte(index))
			if count == 100 {
				zeroLoad := append([]byte{0x41, 0}, rv64MMArg([]byte{0x28}, 50, 0)...)
				funcs = append(funcs, rv64MMFunc{"zero_size", nil, []wasm.ValType{wasm.I32}, []byte{0x3f, 50, 0x0b}}, rv64MMFunc{"zero_grow", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x40, 50, 0x0b}}, rv64MMFunc{"zero_load", nil, []wasm.ValType{wasm.I32}, append(zeroLoad, 0x0b)})
			}
			source := rv64MMModule(funcs, defs, imports, count, nil)
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("count%d-alias%t-artifact%t", count, alias, artifact), func(t *testing.T) {
					c := rv64TableStageCompile(t, fmt.Sprintf("multimemory-grow-%d-%t", count, alias), source, artifact)
					var in *Instance
					if alias {
						m, err := NewSharedMemory(1, 3)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = m.Close() })
						z, err := NewSharedMemory(0, 0)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = z.Close() })
						in = rv64MMInstance(t, c, NewImports().Memory("env", "shared", m).Memory("env", "zero", z))
					} else {
						in = rv64MMInstance(t, c)
					}
					rv64MMCall(t, in, "size", []uint64{1})
					rv64MMCall(t, in, "store", nil, 0xdeadbeef00000003, 0x9999999980000001)
					rv64MMCall(t, in, "load", []uint64{0x80000001}, 3)
					if !alias && binary.LittleEndian.Uint32(rv64MMBytes(t, in, 0)[3:]) != 0 {
						t.Fatal("secondary store reached primary")
					}
					selected := uint64(1)
					if alias {
						selected = 2
					}
					rv64MMCall(t, in, "grow0_size", []uint64{1, selected}, 0xdeadbeef00000001)
					if alias {
						rv64MMCall(t, in, "grow0_store", nil, 2*65536, 77)
						rv64MMCall(t, in, "load", []uint64{77}, 2*65536)
						selected = 3
					}
					rv64MMCall(t, in, "grow", []uint64{selected}, 0xdeadbeef00000000)
					rv64MMCall(t, in, "grow", []uint64{0xffffffff}, 4)
					rv64MMCall(t, in, "size", []uint64{selected})
					if !alias {
						rv64MMCall(t, in, "grow", []uint64{1}, 1)
						rv64MMCall(t, in, "size", []uint64{2})
						rv64MMCall(t, in, "load", []uint64{0}, 65536)
					}
					if count == 100 {
						if len(rv64MMBytes(t, in, 50)) != 0 {
							t.Fatal("zero-capacity memory allocated accessible bytes")
						}
						rv64MMCall(t, in, "zero_size", []uint64{0})
						rv64MMCall(t, in, "zero_grow", []uint64{0}, 0)
						rv64MMCall(t, in, "zero_grow", []uint64{0xffffffff}, 1)
						rv64MMTrap(t, in, "zero_load")
					}
				})
			}
		}
	}
}

func TestRV64PublicMultiMemoryAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source feature admission requires compiler")
	}
	funcs := rv64MMGrowFuncs(99)
	defs := make([][]byte, 100)
	for i := range defs {
		defs[i] = []byte{1, 0, 0}
	}
	source := rv64MMModule(funcs, defs, nil, 100, nil)
	sharedDefs, wideDefs := slices.Clone(defs), slices.Clone(defs)
	sharedDefs[99] = []byte{3, 0, 0}
	wideDefs[99] = []byte{7, 0, 0}
	if c, err := Compile(NewRuntimeConfig().WithCoreFeatures(defaultCoreFeatures()&^CoreFeatureMultiMemory), source); err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("disabled multi-memory admitted")
	}
	for _, tc := range []struct {
		name  string
		defs  [][]byte
		funcs []rv64MMFunc
	}{
		{"too-many", append(slices.Clone(defs), []byte{1, 0, 0}), funcs},
		{"invalid-index", defs, rv64MMGrowFuncs(100)},
		{"unused-shared-secondary", sharedDefs, []rv64MMFunc{{"empty", nil, nil, []byte{0x0b}}}},
		{"unused-shared-memory64-secondary", wideDefs, []rv64MMFunc{{"empty", nil, nil, []byte{0x0b}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(nil, rv64MMModule(tc.funcs, tc.defs, nil, len(tc.defs), nil))
			if c != nil {
				_ = c.Close()
			}
			if err == nil {
				t.Fatal("unsupported source admitted")
			}
		})
	}
}

func TestRV64PublicMultiMemoryArtifactMetadata(t *testing.T) {
	defs := make([][]byte, 100)
	for i := range defs {
		defs[i] = []byte{1, 0, 0}
	}
	source := rv64MMModule(rv64MMGrowFuncs(99), defs, nil, 100, nil)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "multimemory-metadata", source, artifact)
			for _, shared := range []bool{false, true} {
				bad := *c.executionView()
				directory := *bad.memoryDir
				directory.defs = slices.Clone(directory.defs)
				bad.memoryDir = &directory
				if shared {
					directory.defs[99].Shared = true
				} else {
					directory.defs[99].Addr64 = true
					directory.defs[99].Max = 1 << 49
				}
				blob, err := marshalCompiled(&bad)
				if err != nil {
					continue
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					_ = loaded.Close()
				}
				if err == nil {
					t.Fatal("unsupported secondary memory metadata admitted", shared)
				}
			}
		})
	}
}

func TestRV64PublicMultiMemorySecondaryOwnerAndRollback(t *testing.T) {
	providerSource := rv64MMModule(rv64MMGrowFuncs(1), [][]byte{{1, 1, 3}, {1, 1, 3}}, nil, 2, nil)
	imports := [][]byte{rv64MMImport("a", 1, 3), rv64MMImport("b", 1, 3)}
	consumerSource := rv64MMModule(rv64MMGrowFuncs(1), nil, imports, 2, nil)
	// Earlier segment side effects survive a later initializer failure, while
	// borrowed instance context and native trap origin must be restored.
	badSource := rv64MMModule(rv64MMGrowFuncs(1), nil, imports, 2, [][]byte{
		{2, 0, 0x41, 7, 0x0b, 1, 42},
		{2, 1, 0x41, 0xff, 0xff, 3, 0x0b, 2, 8, 9},
	})
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			pc := rv64TableStageCompile(t, "multimemory-owner", providerSource, artifact)
			cc := rv64TableStageCompile(t, "multimemory-borrower", consumerSource, artifact)
			bad := rv64TableStageCompile(t, "multimemory-rollback", badSource, artifact)
			provider := rv64MMInstance(t, pc)
			memory, err := provider.ExportedMemory("m1")
			if err != nil {
				t.Fatal(err)
			}
			bindings := NewImports().Memory("env", "a", memory).Memory("env", "b", memory)
			consumer := rv64MMInstance(t, cc, bindings)
			rv64MMCall(t, provider, "store", nil, 65532, 0x01020304)
			for range 2 {
				before, origin := memory.jobMemory().CaptureInstanceContext(), memory.jobMemory().RV64NativeContext()
				in, err := Instantiate(bad, bindings)
				if in != nil {
					_ = in.Close()
				}
				if err == nil {
					t.Fatal("late secondary initializer succeeded")
				}
				if memory.jobMemory().CaptureInstanceContext() != before || memory.jobMemory().RV64NativeContext() != origin {
					t.Fatal("failed secondary initializer displaced borrowed context/origin")
				}
				if memory.UnsafeBytes()[7] != 42 || memory.UnsafeBytes()[65535] != 1 {
					t.Fatal("initializer rollback lost earlier effects or partly wrote failing segment")
				}
				rv64MMCall(t, consumer, "load", []uint64{0x01020304}, 65532)
			}
			if err := pc.Close(); err != nil {
				t.Fatal(err)
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if memory.jobMemory() == nil {
				t.Fatal("secondary export did not retain retired owner")
			}
			rv64MMCall(t, consumer, "grow0_size", []uint64{1, 2}, 1)
			rv64MMCall(t, consumer, "store", nil, 65536, 99)
			rv64MMCall(t, consumer, "load", []uint64{99}, 65536)
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if memory.jobMemory() != nil {
				t.Fatal("last alias detach retained retired secondary owner")
			}
		})
	}
}

func TestRV64PublicMultiMemorySecondaryQuota(t *testing.T) {
	source := rv64MMModule(rv64MMGrowFuncs(1), nil, [][]byte{rv64MMImport("a", 1, 3), rv64MMImport("b", 1, 3)}, 2, nil)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "multimemory-quota", source, artifact)
			a, err := NewSharedMemory(1, 3)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close() })
			b, err := NewSharedMemory(1, 3)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = b.Close() })
			var consumers []*Instance
			for _, limit := range []uint32{2, 0} {
				rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithMemoryLimitPages(limit)))
				t.Cleanup(func() { _ = rt.Close() })
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = mod.Close() })
				in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Memory("env", "a", a).Memory("env", "b", b)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				consumers = append(consumers, in)
			}
			low, high := consumers[0], consumers[1]
			rv64MMCall(t, low, "grow", []uint64{1}, 1)
			rv64MMCall(t, low, "grow", []uint64{0xffffffff}, 1)
			rv64MMCall(t, high, "grow", []uint64{2}, 1)
			// A stricter tenant sees externally grown size and can query with delta0.
			rv64MMCall(t, low, "size", []uint64{3})
			rv64MMCall(t, low, "grow", []uint64{3}, 0)
			rv64MMCall(t, low, "grow", []uint64{0xffffffff}, 1)
			rv64MMCall(t, low, "store", nil, 2*65536, 123)
			rv64MMCall(t, high, "load", []uint64{123}, 2*65536)
			if len(a.UnsafeBytes()) != 65536 || len(b.UnsafeBytes()) != 3*65536 {
				t.Fatal("secondary growth mutated primary size")
			}
		})
	}
}
