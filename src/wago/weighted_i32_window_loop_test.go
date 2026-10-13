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

func weightedI32WindowModule(n int, bias, div, mul int32, offset uint32, nested bool) []byte {
	declarations := byte(0)
	if nested {
		declarations = 1
	}
	b := []byte{declarations}
	if nested {
		b = append(b, 3, 0x7f)
	}
	idx := func(op byte, x uint32) { b = append(b, op); b = append(b, wasmtest.ULEB(x)...) }
	con := func(x int32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB64(int64(x))...) }
	outer, savedN, savedP := uint32(n+4), uint32(n+5), uint32(n+6)
	if nested {
		idx(0x20, 2)
		idx(0x21, savedN)
		idx(0x20, 0)
		idx(0x21, savedP)
		b = append(b, 0x03, 0x40)
		idx(0x20, savedN)
		idx(0x21, 2)
		idx(0x20, savedP)
		idx(0x21, 0)
	}
	b = append(b, 0x03, 0x40)
	for i := 0; i < n; i++ {
		idx(0x20, uint32(i+4))
		idx(0x20, 0)
		off := int32((n - i - 1) * 4)
		if off != 0 {
			con(off)
			b = append(b, 0x6a)
			if off == 4 {
				idx(0x22, 1)
			}
		}
		b = append(b, 0x28, 2)
		b = append(b, wasmtest.ULEB(offset)...)
		con(bias)
		b = append(b, 0x6a, 0x6c)
	}
	for i := 1; i < n; i++ {
		b = append(b, 0x6a)
	}
	con(div)
	b = append(b, 0x6d)
	idx(0x20, 3)
	b = append(b, 0x73)
	con(mul)
	b = append(b, 0x6c)
	idx(0x21, 3)
	idx(0x20, 1)
	idx(0x21, 0)
	idx(0x20, 2)
	con(-1)
	b = append(b, 0x6a)
	idx(0x22, 2)
	b = append(b, 0x0d, 0, 0x0b)
	if nested {
		idx(0x20, outer)
		con(1)
		b = append(b, 0x6a)
		idx(0x22, outer)
		con(3)
		b = append(b, 0x49, 0x0d, 0, 0x0b)
	}
	for _, x := range []uint32{3, 0, 1, 2} {
		idx(0x20, x)
	}
	b = append(b, 0x0b)
	params := make([]wasm.ValType, n+4)
	for i := range params {
		params[i] = wasm.I32
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
}
func weightedI32WindowOracle(mem []byte, coeff []uint32, ptr, count, hash uint32, bias, div, mul int32, offset uint32, nested bool) ([]uint64, bool) {
	startP, startN := ptr, count
	var next uint32
	reps := 1
	if nested {
		reps = 3
	}
	for rep := 0; rep < reps; rep++ {
		ptr, count = startP, startN
		for iterations := 0; iterations < 20000; iterations++ {
			var sum uint32
			for i, c := range coeff {
				off := uint32((len(coeff) - 1 - i) * 4)
				address := ptr + off
				if off == 4 {
					next = address
				}
				a := uint64(address) + uint64(offset)
				if a+4 > uint64(len(mem)) {
					return nil, true
				}
				v := binary.LittleEndian.Uint32(mem[a:])
				sum += c * (v + uint32(bias))
			}
			q := uint32(int32(sum) / div)
			hash = (q ^ hash) * uint32(mul)
			ptr = next
			count--
			if count == 0 {
				break
			}
			if iterations == 19999 {
				panic("oracle did not terminate or trap")
			}
		}
	}
	return []uint64{uint64(hash), uint64(ptr), uint64(next), uint64(count)}, false
}
func TestWeightedI32WindowPublicExecution(t *testing.T) {
	frames := map[string][]TrapFrame{}
	for _, n := range []int{4, 8, 12, 32} {
		for _, bias := range []int32{-32768, 2147483647, 0} {
			for _, div := range []int32{1, 2, 128, 1 << 30} {
				for _, offset := range []uint32{0, 7, ^uint32(0)} {
					for _, nested := range []bool{false, true} {
						for _, on := range []bool{false, true} {
							for _, abi := range []bool{false, true} {
								t.Run(fmt.Sprintf("terms%d/bias%d/div%d/off%d/nested%t/on%t/abi%t", n, bias, div, offset, nested, on, abi), func(t *testing.T) {
									const mul = int32(-17)
									cfg := NewRuntimeConfig().WithOptimization("reg-abi", abi)
									if runtime.GOARCH == "arm64" {
										cfg = cfg.WithOptimization("integer-sum-loop", on)
									}
									c, e := cfg.Compile(weightedI32WindowModule(n, bias, div, mul, offset, nested))
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
										coeff := make([]uint32, n)
										for i := range coeff {
											coeff[i] = uint32(0xfedcba98 + uint32(i)*0x1020304)
										}
										args := [][2]uint32{{0, 1}, {3, 2}, {128, 3}, {256, 7}, {0, 16}, {uint32(len(mem)) - uint32(n*4) - offset, 1}, {uint32(len(mem)) - uint32(n*4) - offset + 1, 1}, {^uint32(63), 2}, {uint32(len(mem)) - 4, 1}}
										// Exercise long nonterminating/huge-count fallback once per structural case.
										if bias == 0 && div == 1 && offset == 0 {
											args = append(args, [2]uint32{0, 0}, [2]uint32{0, ^uint32(0)})
										}
										for _, a := range args {
											const seed = uint32(0x80000001)
											want, trap := weightedI32WindowOracle(before, coeff, a[0], a[1], seed, bias, div, mul, offset, nested)
											values := make([]uint64, n+4)
											values[0] = uint64(a[0])
											values[1] = 123
											values[2] = uint64(a[1])
											values[3] = uint64(seed)
											for i, c := range coeff {
												values[i+4] = uint64(c)
											}
											for i := range values {
												values[i] |= 0xfedcba9800000000
											}
											got, e := in.Invoke("run", values...)
											if trap {
												var tr *TrapError
												if !errors.As(e, &tr) || tr.Code != TrapLinMemOutOfBounds {
													t.Fatalf("args=%v got=%v err=%v want trap", a, got, e)
												}
												key := fmt.Sprintf("%d/%d/%d/%d/%t/%t/%v", n, bias, div, offset, nested, abi, a)
												if !on {
													frames[key] = slices.Clone(tr.Frames)
												} else if !slices.Equal(tr.Frames, frames[key]) {
													t.Fatalf("trap frames changed args=%v got=%v want=%v", a, tr.Frames, frames[key])
												}
											} else if e != nil || !slices.Equal(got, want) {
												t.Fatalf("args=%v got=%v err=%v want=%v", a, got, e, want)
											}
											if !bytes.Equal(mem, before) {
												t.Fatal("read-only window modified memory")
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
	}
}
