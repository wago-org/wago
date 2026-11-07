//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"math"
	"testing"
)

func TestLoopStoreSinkGuardNativeIntervals(t *testing.T) {
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	args, result, trap := ar.Alloc(64), ar.Alloc(8), ar.Alloc(rt.TrapBufferBytes)
	for _, width := range []uint32{4, 8} {
		for _, stride := range []uint32{1, 8, 16} {
			a := &enc.Asm{}
			for i, r := range []Reg{X20, X21, X15, X19, X25, X24, X22, X27} {
				a.Load64(r, X0, uint32(i*8))
			}
			f := fn{a: a, memSizeReg: X27}
			g := linearLoadSpan{input: X20, counter: X21, address: X9, end: X10, stride: stride, lower: width, extent: width}
			c := cyclicLoadSpan{base: X15, phase: X19, step: X25, limit: X24, shift: 3, extent: 2 * width}
			s := loopStoreSink{output: X22, width: width}
			l := f.emitLinearSpanGuard(g)
			q := f.emitCyclicSpanGuard(g, c)
			z := f.emitLoopStoreSinkGuard(g, c, s)
			fail := append(append(l[:], q[:]...), z[:]...)
			a.MovImm64(X9, 1)
			a.Store64(X9, X3, 0)
			a.Ret()
			cold := a.Len()
			a.MovImm64(X9, 0)
			a.Store64(X9, X3, 0)
			a.Ret()
			for _, pc := range fail {
				if !a.PatchBranch19(pc, cold) {
					t.Fatal("guard range")
				}
			}
			code, entry, err := rt.MapCode(a.B)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []uint64{0, 8, 16, 65528, 65532, 0xfffffff0, 0x8877665500000010} {
				for _, count := range []uint64{0, 1, 7, 0xffffffff, 0x8877665500000001} {
					for _, base := range []uint64{0, 8, 128, 65528, 0xfffffff0, 0x8877665500000080} {
						for _, memory := range []uint64{0, 65536, 1 << 32} {
							limit := uint64(7)
							inEnd := input + (uint64(uint32(count))-1)*uint64(stride) + uint64(width)
							tableEnd := uint64(uint32(base)) + 6*8 + uint64(2*width)
							for _, out := range []uint64{0, 8, input - uint64(width) - uint64(2*width), input - uint64(width), input, input + uint64(width), inEnd - 1, inEnd, inEnd + 1, uint64(uint32(base)) - uint64(2*width), uint64(uint32(base)), tableEnd - 1, tableEnd, 65528, 0xfffffff8, 0x8877665500000080} {
								want := input == uint64(uint32(input)) && uint32(count) != 0 && input >= uint64(width) && inEnd <= memory && tableEnd <= memory && out == uint64(uint32(out)) && out+uint64(2*width) <= memory
								if want {
									want = (out+uint64(2*width) <= input-uint64(width) || out >= inEnd) && (out+uint64(2*width) <= uint64(uint32(base)) || out >= tableEnd)
								}
								for i, v := range []uint64{input, count, base, 0, 1, limit, out, memory} {
									binary.LittleEndian.PutUint64(args[i*8:], v)
								}
								err := eng.Call(entry, args, jm.LinearMemory(), trap, result)
								if err != nil || (binary.LittleEndian.Uint64(result) != 0) != want {
									t.Fatalf("width%d stride%d input%x count%x base%x out%x memory%x got%d want%v err%v", width, stride, input, count, base, out, memory, binary.LittleEndian.Uint64(result), want, err)
								}
							}
						}
					}
				}
			}
			rt.Unmap(code)
		}
	}
}

func storeSinkNativeFixture() (*enc.Asm, linearLoadSpan, cyclicLoadSpan) {
	a := &enc.Asm{}
	a.SubImm32(X9, X20, 4)
	a.AddImm64(X10, X9, 4)
	a.CmpReg64(X10, X27)
	one := a.Bcond(condA)
	a.LdrFIdx(8, linMemReg, X9, 0, false)
	a.Add32(X19, X19, X25)
	a.Subs32(X9, X19, X24)
	a.Csel32(X19, X19, X9, condL)
	a.AddShifted(X23, X15, X19, 3, true)
	a.AddImm64(X9, X23, 8)
	a.CmpReg64(X9, X27)
	table := a.Bcond(condA)
	a.Add64(X16, linMemReg, X23)
	a.LdrF(9, X16, 4, false)
	second := a.Len()
	a.AddImm64(X9, X20, 4)
	a.CmpReg64(X9, X27)
	two := a.Bcond(condA)
	a.LdrFIdx(8, linMemReg, X20, 0, false)
	a.Add64(X16, linMemReg, X22)
	a.StrF(X16, 4, 9, false)
	a.Fadd(8, 8, 8, false)
	a.StrFIdx(linMemReg, X22, 8, 0, false)
	a.AddImm32(X20, X20, 8)
	a.SubsImm32(X21, X21, 1)
	back := a.Bcond(condNE)
	a.Ret()
	cold := a.Len()
	a.Ret()
	for _, pc := range []int{one, table, two} {
		a.PatchBranch19(pc, cold)
	}
	a.PatchBranch19(back, 0)
	g := linearLoadSpan{start: 0, back: back, second: second, input: X20, counter: X21, address: X9, end: X10, stride: 8, lower: 4, extent: 4}
	c, _ := matchCyclicLoadSpan(a.B, g, X27)
	return a, g, c
}

func TestLoopStoreSinkNativeProof(t *testing.T) {
	a, g, c := storeSinkNativeFixture()
	if _, ok := matchLinearLoadSpan(a.B, g.back, X27, nil, nil); !ok || c.extent == 0 {
		t.Fatal("base fixture declined")
	}
	s, ok := matchLoopStoreSink(a.B, g, c, X27)
	if !ok {
		t.Fatal("store fixture declined")
	}
	b := append([]byte{}, a.B...)
	wrWord(b, s.first+4, 0x2a1603f6)
	if _, ok := matchLoopStoreSink(b, g, c, X27); !ok {
		t.Fatal("guard-canonical output copy declined")
	}

	for _, tc := range []struct {
		name string
		pc   int
		word uint32
	}{
		{"extra store", s.first + 4, 0xbd000209}, {"unknown read", 52, 0xbd4002c9}, {"output mutable", s.first + 4, 0x110006d6},
		{"stored FP overwritten", s.first + 4, 0x1e292929}, {"address overwritten", s.first + 4, 0x91000610}, {"address observed", s.first + 4, 0xaa1003e0},
		{"store extent", s.first, rdWord(a.B, s.first) + (1 << 10)}, {"other output", s.second, rdWord(a.B, s.second) ^ (1 << 16)},
		{"extra exit", s.first + 4, 0x54000048}, {"unknown FP opcode", s.first + 4, 0x1e204108},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte{}, a.B...)
			wrWord(b, tc.pc, tc.word)
			if _, ok := matchLoopStoreSink(b, g, c, X27); ok {
				t.Fatal("unsafe store sink admitted")
			}
		})
	}
}

func storeSinkMemoryFixture(t testing.TB, wide bool) *wasm.Module {
	typ, load, store, size, add := byte(0x7d), byte(0x2a), byte(0x38), byte(4), byte(0x92)
	if wide {
		typ, load, store, size, add = 0x7c, 0x2b, 0x39, 8, 0xa0
	}
	// Probe the invariant output extent before the loop, as ordinary compiled
	// entry-dominating bounds facts do. Three FP locals7/8/9, integer index10.
	b := []byte{2, 3, typ, 1, 0x7f, 0x20, 6, load, 0, size, 0x1a,
		0x03, 0x40, 0x20, 0, 0x41, size, 0x6b, load, 0, 0, 0x21, 7,
		0x20, 3, 0x20, 4, 0x6a, 0x21, 3,
		0x20, 3, 0x41, 0, 0x20, 5, 0x20, 3, 0x20, 5, 0x48, 0x1b, 0x6b, 0x21, 3,
		0x20, 2, 0x20, 3, 0x41, 3, 0x74, 0x6a, 0x21, 10,
		0x20, 10, load, 0, size, 0x21, 8,
		0x20, 6, 0x20, 8, store, 0, size,
		0x20, 0, load, 0, 0, 0x21, 9,
		0x20, 7, 0x20, 9, add, 0x21, 7, 0x20, 6, 0x20, 7, store, 0, 0,
		0x20, 0, 0x41, 16, 0x6a, 0x21, 0, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b,
		0x41, 0, 0x20, 0, 0x36, 0, 0, 0x41, 4, 0x20, 1, 0x36, 0, 0, 0x41, 8, 0x20, 3, 0x36, 0, 0, 0x41, 0, 0x0b}
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func TestLoopStoreSinkMemoryAndTraps(t *testing.T) {
	old := commonBoundsLimitEnabled
	commonBoundsLimitEnabled = false
	defer func() { commonBoundsLimitEnabled = old }()
	for _, wide := range []bool{false, true} {
		width := uint64(4)
		if wide {
			width = 8
		}
		initial := make([]byte, 65536)
		for at := uint64(0); at < 65536; at += width {
			v := float64(at/width%31+1) / 8
			if wide {
				binary.LittleEndian.PutUint64(initial[at:], math.Float64bits(v))
			} else {
				binary.LittleEndian.PutUint32(initial[at:], math.Float32bits(float32(v)))
			}
		}
		m := storeSinkMemoryFixture(t, wide)
		cases := [][7]uint64{{64, 1, 1024, 0, 1, 7, 2048}, {64, 7, 1024, 6, 1, 7, 2048}, {64, 7, 1024, 0, 0, 7, 2048}, {64, 7, 1024, 0, 7, 7, 2048},
			{64, 7, 1024, 0, 1, 7, 64 - width}, {64, 7, 1024, 0, 1, 7, 64}, {64, 7, 1024, 0, 1, 7, 72}, {64, 7, 1024, 0, 1, 7, 1024}, {64, 7, 1024, 0, 1, 7, 1032},
			{65520, 7, 1024, 0, 1, 7, 2048}, {65536, 1, 1024, 0, 1, 7, 2048}, {64, 7, 65520, 0, 1, 7, 2048}, {64, 7, 1024, 0, 1, 7, 65532},
			{64, 1, 1024, 0, 1, 0, 2048}, {64, 7, 1024, 7, 1, 7, 2048}, {64, 7, 1024, 0, 8, 7, 2048}, {64, 0, 1024, 0, 1, 7, 2048}, {64, 0xffffffff, 1024, 0, 1, 7, 2048},
			{0, 1, 1024, 0, 1, 7, 2048}, {0xffffffff, 1, 1024, 0, 1, 7, 2048}, {64, 7, 1024, 0, 1, 7, 0x8877665500000800},
			{0x8877665500000040, 0x8877665500000007, 1024, 0, 1, 7, 2048}}
		for _, on := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"linear-load-span": true, "cyclic-load-span": true, "loop-store-sink": on, "leaf-scratch-memsize": false, "memory-min-bounds": false}}
				if diagnosticsEnabled {
					var stats ModuleStats
					opts.Stats = &stats
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					image := append([]byte{}, cm.Code...)
					cm.CodeImage.Close()
					opts.Stats = nil
					if (stats.Funcs[0].Peephole["loop-store-sink"] > 0) != on {
						t.Fatalf("wide%v on%v compact%v stats%v image%x", wide, on, compact, stats.Funcs[0].Peephole, image)
					}
				}
				for _, tc := range cases {
					want := append([]byte{}, initial...)
					input, count, base, phase, step, limit, out := uint32(tc[0]), uint32(tc[1]), uint32(tc[2]), uint32(tc[3]), uint32(tc[4]), uint32(tc[5]), uint64(uint32(tc[6]))
					traps := out+2*width > 65536
					if !traps {
						for n := 0; ; n++ {
							if n > 65536 {
								t.Fatal("oracle did not terminate")
							}
							real := uint64(input - uint32(width))
							if real+width > 65536 {
								traps = true
								break
							}
							realBits := append([]byte{}, want[real:real+width]...)
							phase += step
							if int32(phase) >= int32(limit) {
								phase -= limit
							}
							table := uint64(base + (phase << 3))
							if table+2*width > 65536 {
								traps = true
								break
							}
							tableBits := append([]byte{}, want[table+width:table+2*width]...)
							copy(want[out+width:out+2*width], tableBits)
							if uint64(input)+width > 65536 {
								traps = true
								break
							}
							if wide {
								v := math.Float64frombits(binary.LittleEndian.Uint64(realBits)) + math.Float64frombits(binary.LittleEndian.Uint64(want[uint64(input):]))
								binary.LittleEndian.PutUint64(want[out:], math.Float64bits(v))
							} else {
								v := math.Float32frombits(binary.LittleEndian.Uint32(realBits)) + math.Float32frombits(binary.LittleEndian.Uint32(want[uint64(input):]))
								binary.LittleEndian.PutUint32(want[out:], math.Float32bits(v))
							}
							input += 16
							count--
							if count == 0 {
								break
							}
						}
					}
					if !traps {
						binary.LittleEndian.PutUint32(want, input)
						binary.LittleEndian.PutUint32(want[4:], count)
						binary.LittleEndian.PutUint32(want[8:], phase)
					}
					got, err := runLinearSpanMemory(t, m, opts, initial, tc[:]...)
					if (err != nil) != traps || !bytes.Equal(got, want) {
						t.Fatalf("wide%v on%v compact%v args%x trap%v err%v equal%v", wide, on, compact, tc, traps, err, bytes.Equal(got, want))
					}
				}
			}
		}
	}
}
