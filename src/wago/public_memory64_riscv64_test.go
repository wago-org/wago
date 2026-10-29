//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64M64ULEB(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 127)
		v >>= 7
		if v != 0 {
			b |= 128
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}
func rv64M64Limit(wide bool, min, max uint64) []byte {
	flag := byte(1)
	if wide {
		flag = 5
	}
	out := append([]byte{flag}, rv64M64ULEB(min)...)
	return append(out, rv64M64ULEB(max)...)
}
func rv64M64Arg(op []byte, index uint32, offset uint64) []byte {
	out := append(slices.Clone(op), 0x40)
	out = append(out, wasmtest.ULEB(index)...)
	return append(out, rv64M64ULEB(offset)...)
}
func rv64M64Addr(wide bool) wasm.ValType {
	if wide {
		return wasm.I64
	}
	return wasm.I32
}
func rv64M64Import(name string, wide bool, min, max uint64) []byte {
	out := append(wasmtest.Name("env"), wasmtest.Name(name)...)
	out = append(out, 2)
	return append(out, rv64M64Limit(wide, min, max)...)
}
func rv64M64Data(index uint32, expr, payload []byte) []byte {
	out := append([]byte{2}, wasmtest.ULEB(index)...)
	out = append(out, expr...)
	out = append(out, wasmtest.ULEB(uint32(len(payload)))...)
	return append(out, payload...)
}

type rv64M64Scalar struct {
	name        string
	typ         wasm.ValType
	load, store byte
	width       int
	input, want uint64
}

func rv64M64Scalars() []rv64M64Scalar {
	return []rv64M64Scalar{
		{"i32", wasm.I32, 0x28, 0x36, 4, 0xdeadbeef80000001, 0x80000001}, {"i64", wasm.I64, 0x29, 0x37, 8, 0x80000000deadbeef, 0x80000000deadbeef},
		{"f32", wasm.F32, 0x2a, 0x38, 4, 0x7fa00001, 0x7fa00001}, {"f64", wasm.F64, 0x2b, 0x39, 8, 0x7ff0000000000001, 0x7ff0000000000001},
		{"i32_8s", wasm.I32, 0x2c, 0x3a, 1, 0x80, 0xffffff80}, {"i32_8u", wasm.I32, 0x2d, 0x3a, 1, 0x80, 0x80},
		{"i32_16s", wasm.I32, 0x2e, 0x3b, 2, 0x8001, 0xffff8001}, {"i32_16u", wasm.I32, 0x2f, 0x3b, 2, 0x8001, 0x8001},
		{"i64_8s", wasm.I64, 0x30, 0x3c, 1, 0x80, 0xffffffffffffff80}, {"i64_8u", wasm.I64, 0x31, 0x3c, 1, 0x80, 0x80},
		{"i64_16s", wasm.I64, 0x32, 0x3d, 2, 0x8001, 0xffffffffffff8001}, {"i64_16u", wasm.I64, 0x33, 0x3d, 2, 0x8001, 0x8001},
		{"i64_32s", wasm.I64, 0x34, 0x3e, 4, 0x80000001, 0xffffffff80000001}, {"i64_32u", wasm.I64, 0x35, 0x3e, 4, 0x80000001, 0x80000001},
	}
}

func TestRV64PublicMemory64ScalarMixedWidthsAndCarry(t *testing.T) {
	for _, wideFirst := range []bool{false, true} {
		for _, swar := range []bool{false, true} {
			defs := make([][]byte, 100)
			for i := range defs {
				defs[i] = rv64M64Limit(false, 0, 0)
			}
			defs[0] = rv64M64Limit(wideFirst, 1, 2)
			defs[99] = rv64M64Limit(!wideFirst, 1, 2)
			var funcs []rv64MMFunc
			wideIndex := uint32(99)
			if wideFirst {
				wideIndex = 0
			}
			for _, m := range []uint32{0, 99} {
				wide := m == wideIndex
				for _, tc := range rv64M64Scalars() {
					name := fmt.Sprintf("m%d_%s", m, tc.name)
					load := append([]byte{0x20, 0}, rv64M64Arg([]byte{tc.load}, m, 0)...)
					store := append([]byte{0x20, 0, 0x20, 1}, rv64M64Arg([]byte{tc.store}, m, 0)...)
					funcs = append(funcs, rv64MMFunc{name + "_load", []wasm.ValType{rv64M64Addr(wide)}, []wasm.ValType{tc.typ}, append(load, 0x0b)}, rv64MMFunc{name + "_store", []wasm.ValType{rv64M64Addr(wide), tc.typ}, nil, append(store, 0x0b)})
				}
			}
			offsets := []uint64{1, 253, 1 << 32, 1 << 63, ^uint64(0), (1 << 40) | 253}
			for i, offset := range offsets {
				load := append([]byte{0x20, 0}, rv64M64Arg([]byte{0x28}, wideIndex, offset)...)
				store := append([]byte{0x20, 0, 0x20, 1}, rv64M64Arg([]byte{0x36}, wideIndex, offset)...)
				funcs = append(funcs, rv64MMFunc{fmt.Sprintf("offset%d_load", i), []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, append(load, 0x0b)}, rv64MMFunc{fmt.Sprintf("offset%d_store", i), []wasm.ValType{wasm.I64, wasm.I32}, nil, append(store, 0x0b)})
			}
			source := rv64MMModule(funcs, defs, nil, 100, nil, swar)
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("wideFirst%t-swar%t-artifact%t", wideFirst, swar, artifact), func(t *testing.T) {
					c := rv64TableStageCompile(t, fmt.Sprintf("memory64-scalar-%t-%t", wideFirst, swar), source, artifact)
					in := rv64MMInstance(t, c)
					for _, m := range []uint32{0, 99} {
						wide := m == wideIndex
						memory := rv64MMBytes(t, in, int(m))
						for _, tc := range rv64M64Scalars() {
							name := fmt.Sprintf("m%d_%s", m, tc.name)
							for _, address := range []uint64{3, uint64(65536 - tc.width)} {
								arg := address
								if !wide {
									arg |= 0xdeadbeef00000000
								}
								rv64MMCall(t, in, name+"_store", nil, arg, tc.input)
								rv64MMCall(t, in, name+"_load", []uint64{tc.want}, arg)
							}
							before := slices.Clone(memory[65520:])
							rv64MMTrap(t, in, name+"_store", uint64(65537-tc.width), tc.input)
							rv64MMTrap(t, in, name+"_load", uint64(65537-tc.width))
							if !bytes.Equal(before, memory[65520:]) {
								t.Fatal("partial scalar memory64 store", name)
							}
							if wide {
								for _, address := range []uint64{1 << 32, 1 << 63, ^uint64(0), 0xdeadbeef00000003} {
									rv64MMTrap(t, in, name+"_load", address)
									before := slices.Clone(memory[:16])
									rv64MMTrap(t, in, name+"_store", address, tc.input)
									if !bytes.Equal(before, memory[:16]) {
										t.Fatal("high address truncated", name, address)
									}
								}
							}
						}
					}
					memory := rv64MMBytes(t, in, int(wideIndex))
					rv64MMCall(t, in, "offset1_store", nil, 0, 77)
					rv64MMCall(t, in, "offset1_load", []uint64{77}, 0)
					// These additions wrap to zero, a valid byte address unless carry is checked.
					for _, tc := range []struct {
						i       int
						address uint64
					}{{0, ^uint64(0)}, {2, 0}, {3, 1 << 63}, {4, 1}, {5, 0}} {
						before := slices.Clone(memory[:16])
						rv64MMTrap(t, in, fmt.Sprintf("offset%d_load", tc.i), tc.address)
						rv64MMTrap(t, in, fmt.Sprintf("offset%d_store", tc.i), tc.address, 99)
						if !bytes.Equal(before, memory[:16]) {
							t.Fatal("wrapped effective address wrote memory")
						}
					}
				})
			}
		}
	}
}

func rv64M64GrowFuncs(index byte) []rv64MMFunc {
	load := append([]byte{0x20, 0}, rv64M64Arg([]byte{0x28}, uint32(index), 0)...)
	store := append([]byte{0x20, 0, 0x20, 1}, rv64M64Arg([]byte{0x36}, uint32(index), 0)...)
	return []rv64MMFunc{
		{"size", nil, []wasm.ValType{wasm.I64}, []byte{0x3f, index, 0x0b}},
		{"grow", []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{0x20, 0, 0x40, index, 0x0b}},
		{"load", []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, append(load, 0x0b)},
		{"store", []wasm.ValType{wasm.I64, wasm.I32}, nil, append(store, 0x0b)},
	}
}
func TestRV64PublicMemory64NativeAliasesAndGrowth(t *testing.T) {
	providerSource := rv64MMModule(rv64M64GrowFuncs(1), [][]byte{rv64M64Limit(false, 0, 0), rv64M64Limit(true, 1, 3)}, nil, 2, nil)
	funcs := rv64M64GrowFuncs(99)
	funcs = append(funcs, rv64MMFunc{"alias_grow", []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64, wasm.I64}, []byte{0x20, 0, 0x40, 0, 0x3f, 99, 0x0b}})
	funcs = append(funcs,
		rv64MMFunc{"copy99_0", []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 99, 0, 0x0b}},
		rv64MMFunc{"copy0_99", []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 99, 0x0b}},
		rv64MMFunc{"grow_call_join", []wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, []byte{0x20, 1, 0x04, 0x7e, 0x20, 0, 0x10, 1, 0x05, 0x42, 0, 0x0b, 0x0b}},
	)
	imports := make([][]byte, 100)
	for i := range imports {
		if i == 0 || i == 99 {
			imports[i] = rv64M64Import("wide", true, 1, 3)
		} else {
			imports[i] = rv64M64Import("zero", false, 0, 0)
		}
	}
	for _, swar := range []bool{false, true} {
		source := rv64MMModule(funcs, nil, imports, 100, nil, swar)
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar%t-artifact%t", swar, artifact), func(t *testing.T) {
				pc := rv64TableStageCompile(t, "memory64-native-provider", providerSource, artifact)
				provider := rv64MMInstance(t, pc)
				wide, err := provider.ExportedMemory("m1")
				if err != nil {
					t.Fatal(err)
				}
				zero, err := provider.ExportedMemory("m0")
				if err != nil {
					t.Fatal(err)
				}
				cc := rv64TableStageCompile(t, fmt.Sprintf("memory64-native-alias-%t", swar), source, artifact)
				wrong, err := NewSharedMemory(1, 3)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = wrong.Close() })
				if invalid, err := Instantiate(cc, NewImports().Memory("env", "wide", wrong).Memory("env", "zero", zero)); invalid != nil || err == nil {
					if invalid != nil {
						_ = invalid.Close()
					}
					t.Fatal("memory32 host object satisfied a memory64 import")
				}
				if cc.codeCache.refs != 0 {
					t.Fatal("width mismatch acquired executable code")
				}
				a := rv64MMInstance(t, cc, NewImports().Memory("env", "wide", wide).Memory("env", "zero", zero))
				b := rv64MMInstance(t, cc, NewImports().Memory("env", "wide", wide).Memory("env", "zero", zero))
				for _, name := range []string{"copy99_0", "copy0_99"} {
					for _, pair := range [][2]int{{4, 0}, {0, 4}} {
						memory := wide.UnsafeBytes()
						for i := 0; i < 64; i++ {
							memory[i] = byte(i)
						}
						expected := slices.Clone(memory[:64])
						copy(expected[pair[0]:pair[0]+24], expected[pair[1]:pair[1]+24])
						rv64MMCall(t, a, name, nil, uint64(pair[0]), uint64(pair[1]), 24)
						if !bytes.Equal(expected, memory[:64]) {
							t.Fatal("memory64 alias memmove", name, pair)
						}
					}
				}
				rv64MMCall(t, a, "alias_grow", []uint64{1, 2}, 1)
				rv64MMCall(t, b, "size", []uint64{2})
				rv64MMCall(t, a, "grow_call_join", []uint64{^uint64(0)}, 1<<32, 0xdeadbeef00000001)
				rv64MMCall(t, a, "grow_call_join", []uint64{2}, 0, 1)
				if allocations := testing.AllocsPerRun(20, func() {
					got, err := a.Invoke("grow", 0)
					if err != nil || len(got) != 1 || got[0] != 2 {
						panic("memory64 zero growth")
					}
				}); allocations != 0 {
					t.Fatal("warm memory64 grow allocated", allocations)
				}
				for _, delta := range []uint64{1 << 32, 1 << 63, ^uint64(0), ^uint64(0) - 1} {
					rv64MMCall(t, a, "grow", []uint64{^uint64(0)}, delta)
					rv64MMCall(t, b, "size", []uint64{2})
				}
				rv64MMCall(t, b, "store", nil, 65536, 0xdeadbeef80000001)
				rv64MMCall(t, a, "load", []uint64{0x80000001}, 65536)
				rv64MMCall(t, a, "grow", []uint64{2}, 0)
				rv64MMCall(t, a, "grow", []uint64{2}, 1)
				rv64MMCall(t, b, "grow", []uint64{^uint64(0)}, 1)
				if err := provider.Close(); err != nil {
					t.Fatal(err)
				}
				if err := pc.Close(); err != nil {
					t.Fatal(err)
				}
				rv64MMCall(t, a, "store", nil, 2*65536, 71)
				rv64MMCall(t, b, "load", []uint64{71}, 2*65536)
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				if wide.jobMemory() == nil {
					t.Fatal("first alias consumer lost provider")
				}
				rv64MMCall(t, b, "size", []uint64{3})
				if err := b.Close(); err != nil {
					t.Fatal(err)
				}
				if wide.jobMemory() != nil || zero.jobMemory() != nil {
					t.Fatal("final alias release retained provider")
				}
			})
		}
	}
}

func rv64M64BulkFuncs(widths []bool) []rv64MMFunc {
	var funcs []rv64MMFunc
	for dst, wide := range widths {
		for src, sourceWide := range widths {
			count := wasm.I32
			if wide && sourceWide {
				count = wasm.I64
			}
			funcs = append(funcs, rv64MMFunc{fmt.Sprintf("copy%d%d", dst, src), []wasm.ValType{rv64M64Addr(wide), rv64M64Addr(sourceWide), count}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, byte(dst), byte(src), 0x0b}})
		}
		funcs = append(funcs, rv64MMFunc{fmt.Sprintf("fill%d", dst), []wasm.ValType{rv64M64Addr(wide), wasm.I32, rv64M64Addr(wide)}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, byte(dst), 0x0b}}, rv64MMFunc{fmt.Sprintf("init%d", dst), []wasm.ValType{rv64M64Addr(wide), wasm.I32, wasm.I32}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 8, 1, byte(dst), 0x0b}})
	}
	return append(funcs, rv64MMFunc{"drop", nil, nil, []byte{0xfc, 9, 1, 0x0b}})
}
func TestRV64PublicMemory64BulkOperandClasses(t *testing.T) {
	for _, wideFirst := range []bool{false, true} {
		widths := []bool{wideFirst, !wideFirst}
		defs := [][]byte{rv64M64Limit(widths[0], 1, 1), rv64M64Limit(widths[1], 1, 1)}
		expr := []byte{0x41, 0, 0x0b}
		if widths[1] {
			expr[0] = 0x42
		}
		data := [][]byte{rv64M64Data(1, expr, []byte{42}), {1, 4, 0x80, 0x81, 0xfe, 0xff}}
		for _, swar := range []bool{false, true} {
			source := rv64MMModule(rv64M64BulkFuncs(widths), defs, nil, 2, data, swar)
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("wideFirst%t-swar%t-artifact%t", wideFirst, swar, artifact), func(t *testing.T) {
					c := rv64TableStageCompile(t, fmt.Sprintf("memory64-bulk-%t-%t", wideFirst, swar), source, artifact)
					in := rv64MMInstance(t, c)
					memory := [][]byte{rv64MMBytes(t, in, 0), rv64MMBytes(t, in, 1)}
					if memory[1][0] != 42 {
						t.Fatal("typed active initializer")
					}
					for dst := 0; dst < 2; dst++ {
						for src := 0; src < 2; src++ {
							name := fmt.Sprintf("copy%d%d", dst, src)
							wideCount := widths[dst] && widths[src]
							for _, pair := range [][2]int{{4, 0}, {0, 4}, {0, 0}} {
								for i := 0; i < 64; i++ {
									memory[0][i] = byte(i)
									memory[1][i] = byte(i + 64)
								}
								expected := slices.Clone(memory[dst][:64])
								copy(expected[pair[0]:], slices.Clone(memory[src][pair[1]:pair[1]+24]))
								args := []uint64{uint64(pair[0]), uint64(pair[1]), 24}
								for i, wide := range []bool{widths[dst], widths[src], wideCount} {
									if !wide {
										args[i] |= 0xdeadbeef00000000
									}
								}
								rv64MMCall(t, in, name, nil, args...)
								if !bytes.Equal(expected, memory[dst][:64]) {
									t.Fatal("mixed copy/overlap", name, pair)
								}
							}
							before := slices.Clone(memory[dst][:64])
							rv64MMTrap(t, in, name, 0, 65535, 2)
							if !bytes.Equal(before, memory[dst][:64]) {
								t.Fatal("source bounds partially copied", name)
							}
							rv64MMCall(t, in, name, nil, 65536, 65536, 0)
							rv64MMTrap(t, in, name, 65537, 65536, 0)
							rv64MMTrap(t, in, name, 65536, 65537, 0)
							if wideCount {
								rv64MMTrap(t, in, name, 0, 0, 1<<32)
								rv64MMTrap(t, in, name, 0, 0, ^uint64(0))
							}
							if widths[dst] {
								rv64MMTrap(t, in, name, 1<<32, 0, 0)
							}
							if widths[src] {
								rv64MMTrap(t, in, name, 0, 1<<32, 0)
							}
						}
					}
					for m, wide := range widths {
						addr, count := uint64(3), uint64(7)
						if !wide {
							addr |= 0xaabbccdd00000000
							count |= 0xdeadbeef00000000
						}
						rv64MMCall(t, in, fmt.Sprintf("fill%d", m), nil, addr, 0xffffffff12345680, count)
						if !bytes.Equal(memory[m][3:10], bytes.Repeat([]byte{0x80}, 7)) {
							t.Fatal("fill byte/count width")
						}
						rv64MMCall(t, in, fmt.Sprintf("init%d", m), nil, 20, 0xdeadbeef00000001, 0xfeedface00000003)
						if !bytes.Equal(memory[m][20:23], data[1][3:]) {
							t.Fatal("memory.init source/count must remain I32")
						}
						tail := slices.Clone(memory[m][65520:])
						rv64MMTrap(t, in, fmt.Sprintf("fill%d", m), 65535, 99, 2)
						rv64MMTrap(t, in, fmt.Sprintf("init%d", m), 65535, 0, 2)
						if !bytes.Equal(tail, memory[m][65520:]) {
							t.Fatal("bulk partial write")
						}
						if wide {
							rv64MMTrap(t, in, fmt.Sprintf("fill%d", m), 0, 99, 1<<32)
							rv64MMTrap(t, in, fmt.Sprintf("init%d", m), 1<<32, 0, 0)
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

func TestRV64PublicMemory64SWAR(t *testing.T) {
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
	for _, selected := range []uint32{0, 99} {
		var funcs []rv64MMFunc
		for _, tc := range cases {
			params := []wasm.ValType{wasm.I64}
			results := []wasm.ValType{wasm.V128}
			body := []byte{0x20, 0}
			if tc.lane >= 0 || tc.store {
				params = append(params, wasm.V128)
				body = append(body, 0x20, 1)
			}
			body = append(body, rv64M64Arg([]byte{0xfd, tc.sub}, selected, 0)...)
			if tc.lane >= 0 {
				body = append(body, byte(tc.lane))
			}
			body = append(body, 0x0b)
			if tc.store {
				results = nil
			}
			funcs = append(funcs, rv64MMFunc{tc.name, params, results, body})
			carry := []byte{0x20, 0}
			if tc.lane >= 0 || tc.store {
				carry = append(carry, 0x20, 1)
			}
			carry = append(carry, rv64M64Arg([]byte{0xfd, tc.sub}, selected, 1)...)
			if tc.lane >= 0 {
				carry = append(carry, byte(tc.lane))
			}
			carry = append(carry, 0x0b)
			funcs = append(funcs, rv64MMFunc{tc.name + "_carry", params, results, carry})
		}
		defs := make([][]byte, 100)
		for i := range defs {
			defs[i] = rv64M64Limit(false, 0, 0)
		}
		defs[0] = rv64M64Limit(selected == 0, 1, 1)
		defs[99] = rv64M64Limit(selected == 99, 1, 1)
		source := rv64MMModule(funcs, defs, nil, 100, nil)
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("index%d-artifact%t", selected, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("memory64-swar-%d", selected), source, artifact)
				in := rv64MMInstance(t, c)
				memory := rv64MMBytes(t, in, int(selected))
				other := rv64MMBytes(t, in, int(99-selected))
				seed := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
				input := [16]byte{0x80, 0xfe, 0x7f, 0xff, 0x81, 0x80, 0, 0xff, 9, 10, 11, 12, 13, 14, 15, 16}
				for _, tc := range cases {
					for _, address := range []int{3, 65536 - tc.width} {
						copy(memory[address:], input[:tc.width])
						beforeOther := slices.Clone(other[address : address+tc.width])
						expected := make([]byte, 16)
						args := []uint64{uint64(address)}
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
						args[0] = 1 << 32
						rv64MMTrap(t, in, tc.name, args...)
						args[0] = ^uint64(0)
						rv64MMTrap(t, in, tc.name, args...)
						beforeCarry := slices.Clone(memory[:16])
						rv64MMTrap(t, in, tc.name+"_carry", args...)
						if !bytes.Equal(beforeCarry, memory[:16]) {
							t.Fatal("vector carry partially wrote", tc.name)
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
}

func TestRV64PublicMemory64Initializers(t *testing.T) {
	defs := make([][]byte, 100)
	for i := range defs {
		defs[i] = rv64M64Limit(false, 0, 0)
	}
	defs[99] = rv64M64Limit(true, 1, 1)
	source := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "offset", wasm.I64, false))),
		wasmtest.Section(5, wasmtest.Vec(defs...)),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 0, 0x42, 1, 0x7c, 0x0b}), wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 1, 0x42, 1, 0x7c, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("m99", 2, 99))),
		wasmtest.Section(11, wasmtest.Vec(rv64M64Data(99, []byte{0x42, 3, 0x0b}, []byte{0xaa}), rv64M64Data(99, []byte{0x23, 0, 0x0b}, []byte{0xbb}), rv64M64Data(99, []byte{0x23, 2, 0x42, 2, 0x7c, 0x0b}, []byte{0xcc}))),
	)
	data := make([][]byte, 256)
	for i := range data {
		data[i] = []byte{1, 0}
	}
	data[255] = []byte{1, 2, 0xde, 0xad}
	initSource := rv64MMModule([]rv64MMFunc{{"init", []wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 8, 0xff, 1, 99, 0x0b}}}, defs, nil, 100, data)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "memory64-initializers", source, artifact)
			g := NewGlobalI64(5, false)
			t.Cleanup(func() { _ = g.Close() })
			in := rv64MMInstance(t, c, NewImports().Global("env", "offset", g))
			memory := rv64MMBytes(t, in, 99)
			if memory[3] != 0xaa || memory[5] != 0xbb || memory[9] != 0xcc {
				t.Fatal("I64 literal/import/prior-local extended offsets")
			}
			if !c.hasNoFunctionCode() || c.codeCache.mem != nil {
				t.Fatal("state-only memory64 allocated code")
			}
			ic := rv64TableStageCompile(t, "memory64-data-index255", initSource, artifact)
			ii := rv64MMInstance(t, ic)
			rv64MMCall(t, ii, "init", nil, 7, 0xdeadbeef00000000, 0xfeedface00000002)
			if !bytes.Equal(rv64MMBytes(t, ii, 99)[7:9], []byte{0xde, 0xad}) {
				t.Fatal("original data255/memory99 lost")
			}
		})
	}
}

func TestRV64PublicMemory64ExactMaximumMetadata(t *testing.T) {
	for _, maximum := range []uint64{0, 3, 1 << 32, 1 << 48} {
		defs := [][]byte{rv64M64Limit(false, 0, 0), rv64M64Limit(true, 0, maximum)}
		source := rv64MMModule(nil, defs, nil, 2, nil)
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("max%d-artifact%t", maximum, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("memory64-max-%d", maximum), source, artifact)
				def := c.memoryDef(1)
				if !def.Addr64 || !def.HasMax || def.Max != maximum {
					t.Fatal("exact declared maximum narrowed", def)
				}
				// Do not instantiate giant reservations in this metadata regression. The
				// shared reservation tests independently check the finite physical cap.
				if maximum <= 3 {
					in := rv64MMInstance(t, c)
					if len(rv64MMBytes(t, in, 1)) != 0 {
						t.Fatal("zero-minimum memory has live pages")
					}
				}
				if maximum == 1<<48 {
					if err := applyPolicy(&Module{c: c}, Policy{MaxMemoryBytes: ^uint64(0)}); err == nil {
						t.Fatal("declared byte size overflow was not rejected")
					}
				}
			})
		}
	}
}

func TestRV64PublicMemory64InitializerRollback(t *testing.T) {
	providerSource := rv64MMModule(rv64M64GrowFuncs(0), [][]byte{rv64M64Limit(true, 1, 1)}, nil, 1, nil)
	expr := append([]byte{0x42}, wasmtest.SLEB64(65535)...)
	expr = append(expr, 0x0b)
	source := rv64MMModule(nil, nil, [][]byte{rv64M64Import("a", true, 1, 1), rv64M64Import("b", true, 1, 1)}, 2, [][]byte{rv64M64Data(0, []byte{0x42, 5, 0x0b}, []byte{42}), rv64M64Data(1, expr, []byte{8, 9})})
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			pc := rv64TableStageCompile(t, "memory64-rollback-provider", providerSource, artifact)
			provider := rv64MMInstance(t, pc)
			memory, err := provider.ExportedMemory("m0")
			if err != nil {
				t.Fatal(err)
			}
			memory.UnsafeBytes()[65535] = 99
			c := rv64TableStageCompile(t, "memory64-rollback", source, artifact)
			for range 2 {
				before, origin := provider.jm.CaptureInstanceContext(), provider.jm.RV64NativeContext()
				in, err := Instantiate(c, NewImports().Memory("env", "a", memory).Memory("env", "b", memory))
				if in != nil {
					_ = in.Close()
				}
				if err == nil {
					t.Fatal("late memory64 initializer succeeded")
				}
				if provider.jm.CaptureInstanceContext() != before || provider.jm.RV64NativeContext() != origin {
					t.Fatal("memory64 initializer retained borrowed context")
				}
				if memory.UnsafeBytes()[5] != 42 || memory.UnsafeBytes()[65535] != 99 {
					t.Fatal("initializer lost prior effects or partially wrote")
				}
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if memory.jobMemory() != nil {
				t.Fatal("failed memory64 initializer leaked owner attachment")
			}
		})
	}
}
