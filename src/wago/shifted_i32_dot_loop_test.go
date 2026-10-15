//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"slices"
	"testing"
)

func shiftedI32DotModule(lb, rb int32, offset uint32, nested bool, pressure ...bool) []byte {
	count := uint32(1)
	if nested {
		count = 4
	}
	if len(pressure) != 0 && pressure[0] {
		count += 7
	}
	b := append([]byte{1}, wasmtest.ULEB(count)...)
	b = append(b, 0x7f)
	idx := func(op byte, x uint32) { b = append(b, op); b = append(b, wasmtest.ULEB(x)...) }
	con := func(x int32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB64(int64(x))...) }
	load := func() { b = append(b, 0x28, 2); b = append(b, wasmtest.ULEB(offset)...) }
	bias := func(x int32) { con(x); b = append(b, 0x6a) }
	if len(pressure) != 0 && pressure[0] {
		b = append(b, 0xfd, 0x0c)
		b = append(b, make([]byte, 16)...)
		b = append(b, 0x1a)
		for x := uint32(6); x < 13; x++ {
			idx(0x20, 0)
			idx(0x21, x)
		}
	}

	if nested {
		idx(0x20, 3)
		idx(0x21, 7)
		idx(0x20, 0)
		idx(0x21, 8)
		b = append(b, 0x03, 0x40)
		idx(0x20, 7)
		idx(0x21, 3)
		idx(0x20, 8)
		idx(0x21, 0)
	}
	b = append(b, 0x03, 0x40)
	idx(0x20, 0)
	load()
	bias(lb)
	idx(0x20, 0)
	idx(0x20, 1)
	b = append(b, 0x6a)
	idx(0x22, 5)
	con(4)
	b = append(b, 0x6a)
	load()
	bias(rb)
	b = append(b, 0x6c)
	idx(0x20, 0)
	con(-4)
	b = append(b, 0x6a)
	load()
	bias(lb)
	idx(0x20, 5)
	load()
	bias(rb)
	b = append(b, 0x6c)
	idx(0x20, 4)
	b = append(b, 0x6a, 0x6a)
	idx(0x21, 4)
	idx(0x20, 0)
	con(8)
	b = append(b, 0x6a)
	idx(0x21, 0)
	idx(0x20, 2)
	idx(0x20, 3)
	con(-2)
	b = append(b, 0x6a)
	idx(0x22, 3)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	if nested {
		idx(0x20, 6)
		con(1)
		b = append(b, 0x6a)
		idx(0x22, 6)
		con(3)
		b = append(b, 0x49, 0x0d, 0, 0x0b)
	}
	results := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	if len(pressure) != 0 && pressure[0] {
		idx(0x20, 4)
		con(lb) // Observe the restored function-wide literal after the join.
		b = append(b, 0x6a)
		for x := uint32(6); x < 13; x++ {
			idx(0x20, x)
			b = append(b, 0x6a)
		}
		results = results[:1]
	} else {
		for _, x := range []uint32{4, 0, 3, 5} {
			idx(0x20, x)
		}
	}
	b = append(b, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, results))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
}
func shiftedI32DotOracle(mem []byte, args [5]uint32, lb, rb int32, offset uint32, nested bool) ([]uint64, bool) {
	ptr, shift, limit, counter, acc := args[0], args[1], args[2], args[3], args[4]
	var right uint32
	load := func(a uint32) (uint32, bool) {
		start := uint64(a) + uint64(offset)
		if start+4 > uint64(len(mem)) {
			return 0, false
		}
		return binary.LittleEndian.Uint32(mem[start:]), true
	}
	reps := 1
	if nested {
		reps = 3
	}
	for rep := 0; rep < reps; rep++ {
		ptr, counter = args[0], args[3]
		for n := 0; n < 10000; n++ {
			lh, ok := load(ptr)
			if !ok {
				return nil, true
			}
			right = ptr + shift
			rh, ok := load(right + 4)
			if !ok {
				return nil, true
			}
			ll, ok := load(ptr - 4)
			if !ok {
				return nil, true
			}
			rl, ok := load(right)
			if !ok {
				return nil, true
			}
			acc += (lh+uint32(lb))*(rh+uint32(rb)) + (ll+uint32(lb))*(rl+uint32(rb))
			ptr += 8
			counter -= 2
			if counter == limit {
				break
			}
			if n == 9999 {
				panic("oracle did not trap or terminate")
			}
		}
	}
	return []uint64{uint64(acc), uint64(ptr), uint64(counter), uint64(right)}, false
}
func TestShiftedI32DotPublicExecution(t *testing.T) {
	traps := map[string][]TrapFrame{}
	for _, bias := range [][2]int32{{-128, -128}, {2147483647, -2147483648}, {0, 1}} {
		for _, offset := range []uint32{0, 7, ^uint32(0)} {
			for _, nested := range []bool{false, true} {
				for _, on := range []bool{false, true} {
					for _, abi := range []bool{false, true} {
						t.Run(fmt.Sprintf("bias=%v/off=%d/nested=%t/on=%t/abi=%t", bias, offset, nested, on, abi), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithOptimization("reg-abi", abi)
							if runtime.GOARCH == "arm64" {
								cfg = cfg.WithOptimization("integer-sum-loop", on)
							}
							c, e := cfg.Compile(shiftedI32DotModule(bias[0], bias[1], offset, nested))
							if e != nil {
								t.Fatal(e)
							}
							defer c.Close()
							cases := []*Compiled{c}
							if !guardPageBuilt {
								loaded := publicArtifactRoundTrip(t, c)
								defer loaded.Close()
								cases = append(cases, loaded)
							}
							for _, compiled := range cases {
								in, e := Instantiate(compiled)
								if e != nil {
									t.Fatal(e)
								}
								defer in.Close()
								mem := in.Memory().UnsafeBytes()
								for i := range mem {
									mem[i] = byte(i*37 ^ (i >> 3) ^ (i >> 9))
								}
								before := bytes.Clone(mem)
								argsList := [][5]uint32{}
								for _, pairs := range []uint32{1, 2, 3, 4, 7, 8, 15, 16, 17, 31, 32, 33, 64} {
									for _, start := range []uint32{0, 1, 2, ^uint32(0), 0x80000001} {
										for _, ps := range [][2]uint32{{4, 0}, {7, 13}, {128, ^uint32(3)}, {256, ^uint32(255)}} {
											argsList = append(argsList, [5]uint32{ps[0], ps[1], start - 2*pairs, start, 0xfedcba98})
										}
									}
								}
								argsList = append(argsList, [5]uint32{4, 0, 0, 0, 0}, [5]uint32{4, 0, 1, 0, 0}, [5]uint32{0, 0, ^uint32(1), 0, 0}, [5]uint32{uint32(len(mem)) - 4, 0, ^uint32(1), 0, 0}, [5]uint32{uint32(len(mem)) - 4, 4 - uint32(len(mem)), ^uint32(1), 0, 0}, [5]uint32{^uint32(3), 8, ^uint32(1), 0, 0}, [5]uint32{4, ^uint32(7), ^uint32(1), 0, 0})
								for _, args := range argsList {
									want, trap := shiftedI32DotOracle(before, args, bias[0], bias[1], offset, nested)
									carrier := make([]uint64, 5)
									for i, a := range args {
										carrier[i] = uint64(a) | 0xfedcba9800000000
									}
									got, e := in.Invoke("run", carrier...)
									if trap {
										var tr *TrapError
										if !errors.As(e, &tr) || tr.Code != TrapLinMemOutOfBounds {
											t.Fatalf("args=%v want trap got=%v err=%v", args, got, e)
										}
										key := fmt.Sprintf("%v/%d/%t/%t/%v", bias, offset, nested, abi, args)
										if !on {
											traps[key] = slices.Clone(tr.Frames)
										} else if !slices.Equal(tr.Frames, traps[key]) {
											t.Fatalf("args=%v trap frames changed: %v vs %v", args, tr.Frames, traps[key])
										}
									} else if e != nil || !slices.Equal(got, want) {
										t.Fatalf("args=%v got=%v err=%v want=%v", args, got, e, want)
									}
									if !bytes.Equal(mem, before) {
										t.Fatal("read-only loop modified memory")
									}
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestShiftedI32DotLiteralCachePressure(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("ARM64 vector lowering")
	}
	const bias int32 = 0x1234567
	for _, on := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("integer-sum-loop", on).Compile(shiftedI32DotModule(bias, bias, 0, false, true))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		restored := publicArtifactRoundTrip(t, c)
		defer restored.Close()
		for _, code := range []*Compiled{c, restored} {
			in, err := Instantiate(code)
			if err != nil {
				t.Fatal(err)
			}
			mem := in.Memory().UnsafeBytes()
			for i := range mem {
				mem[i] = byte(i*37 ^ (i >> 3))
			}
			for _, count := range []uint32{2, 4, 6, 8, 14, 16, 18, 64} {
				for _, ptr := range []uint32{0, 4, 7, uint32(len(mem)) - 4} {
					args := [5]uint32{ptr, 0, 0, count, 0xfedcba98}
					want, trap := shiftedI32DotOracle(mem, args, bias, bias, 0, false)
					got, err := in.Invoke("run", uint64(ptr), 0, 0, uint64(count), uint64(args[4]))
					if trap {
						var tr *TrapError
						if !errors.As(err, &tr) || tr.Code != TrapLinMemOutOfBounds {
							in.Close()
							t.Fatalf("on=%t ptr=%d count=%d expected trap, got %v %v", on, ptr, count, got, err)
						}
						continue
					}
					expected := uint64(uint32(want[0]) + uint32(bias) + 7*ptr)
					if err != nil || len(got) != 1 || got[0] != expected {
						in.Close()
						t.Fatalf("on=%t ptr=%d count=%d got=%v want=%d err=%v", on, ptr, count, got, expected, err)
					}
				}
			}
			in.Close()
		}
	}
}
