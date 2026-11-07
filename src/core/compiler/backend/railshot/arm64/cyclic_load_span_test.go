//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestCyclicLoadSpanGuardNativeMath(t *testing.T) {
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
	args, result, trap := ar.Alloc(40), ar.Alloc(8), ar.Alloc(rt.TrapBufferBytes)
	for _, shift := range []uint8{0, 1, 3, 4} {
		a := &enc.Asm{}
		for i, r := range []Reg{X24, X25, X19, X15, X27} {
			a.Load64(r, X0, uint32(i*8))
		}
		f := fn{a: a, memSizeReg: X27}
		g := linearLoadSpan{address: X9, end: X10}
		c := cyclicLoadSpan{limit: X24, step: X25, phase: X19, base: X15, shift: shift, extent: 8}
		fail := f.emitCyclicSpanGuard(g, c)
		a.MovImm64(X9, 1)
		a.Store64(X9, X3, 0)
		a.Ret()
		cold := a.Len()
		a.MovImm64(X9, 0)
		a.Store64(X9, X3, 0)
		a.Ret()
		for _, at := range fail {
			if !a.PatchBranch19(at, cold) {
				t.Fatal("guard range")
			}
		}
		code, entry, err := rt.MapCode(a.B)
		if err != nil {
			t.Fatal(err)
		}
		for _, limit := range []uint64{0, 1, 2, 7, 1 << 30, 1<<30 + 1, 1 << 31, 0xffffffff, 0x8877665500000007} {
			for _, step := range []uint64{0, 1, 2, 7, limit, limit + 1, 0x80000000, 0x8877665500000001} {
				for _, phase := range []uint64{0, 1, 6, uint64(uint32(limit) - 1), limit, 0x80000000, 0xffffffff, 0x8877665500000001} {
					for _, base := range []uint64{0, 8, 65528, 0xfffffff0, 0xfffffff8, 0xffffffff, 0x8877665500000008} {
						for _, memory := range []uint64{0, 65536, 1 << 32} {
							l, s, p, b := uint64(uint32(limit)), uint64(uint32(step)), uint64(uint32(phase)), uint64(uint32(base))
							want := l >= 1 && l <= 1<<30 && s <= l && p < l && b+(l-1)*(uint64(1)<<shift)+8 <= memory
							for i, v := range []uint64{limit, step, phase, base, memory} {
								binary.LittleEndian.PutUint64(args[i*8:], v)
							}
							err := eng.Call(entry, args, jm.LinearMemory(), trap, result)
							if err != nil || (binary.LittleEndian.Uint64(result) != 0) != want {
								t.Fatalf("shift%d limit%x step%x phase%x base%x memory%x want%v got%d err%v", shift, limit, step, phase, base, memory, want, binary.LittleEndian.Uint64(result), err)
							}
						}
					}
				}
			}
		}
		rt.Unmap(code)
	}
}

func cyclicSpanNativeFixture() (*enc.Asm, linearLoadSpan, int) {
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
	a.LdrFIdx(9, linMemReg, X23, 0, false)
	second := a.Len()
	a.AddImm64(X9, X20, 4)
	a.CmpReg64(X9, X27)
	two := a.Bcond(condA)
	a.LdrFIdx(10, linMemReg, X20, 0, false)
	a.AddImm32(X20, X20, 8)
	a.SubsImm32(X21, X21, 1)
	back := a.Bcond(condNE)
	a.Ret()
	cold := a.Len()
	a.Ret()
	for _, at := range []int{one, table, two} {
		a.PatchBranch19(at, cold)
	}
	a.PatchBranch19(back, 0)
	return a, linearLoadSpan{start: 0, back: back, second: second, input: X20, counter: X21, address: X9, end: X10, stride: 8, lower: 4, extent: 4}, back
}

func TestCyclicLoadSpanNativeProof(t *testing.T) {
	a, g, back := cyclicSpanNativeFixture()
	if _, ok := matchLinearLoadSpan(a.B, back, X27, nil, nil); !ok {
		t.Fatal("linear fixture declined")
	}
	c, ok := matchCyclicLoadSpan(a.B, g, X27)
	if !ok || c.check != 36 {
		t.Fatalf("cyclic fixture declined: %+v %v", c, ok)
	}
	for _, tc := range []struct {
		name string
		pc   int
		word uint32
	}{
		{"phase overwrite", 48, 0x11000673}, {"step overwrite", 48, 0x11000739}, {"limit overwrite", 48, 0x11000718}, {"base overwrite", 48, 0x110005ef},
		{"index overwrite", 48, 0x110006f7}, {"signed condition", 28, 0x1a89a273}, {"shift too large", 32, 0x0b1351f7},
		{"guard alias input", 20, 0x0b190294}, {"scratch alias phase", 24, 0x6b180273}, {"wrong extent", 36, 0x914022e9},
		{"flags observed after removed check", 48, 0x1a9f17e8}, {"unknown call", 48, 0x94000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte{}, a.B...)
			wrWord(b, tc.pc, tc.word)
			if _, ok := matchCyclicLoadSpan(b, g, X27); ok {
				t.Fatal("unsafe cyclic proof admitted")
			}
		})
	}
	if !spanEndsDeadCyclic(a.B, g, nil, c) {
		t.Fatal("dead native end values not recognized")
	}
	// Removing the table ADD must not discard a value observed on any exit.
	b := append([]byte{}, a.B...)
	wrWord(b, g.back+4, 0xaa0903e0)
	if spanEndsDeadCyclic(b, g, nil, c) {
		t.Fatal("visible table end discarded")
	}
}

func cyclicSpanMemoryFixture(t testing.TB, wide bool, shift byte) *wasm.Module {
	typ, load, store, size := byte(0x7d), byte(0x2a), byte(0x38), byte(4)
	if wide {
		typ, load, store, size = 0x7c, 0x2b, 0x39, 8
	}
	// Seven i32 parameters: input,count,table,phase,step,limit,output.
	// Float locals7/8 and integer index9.
	b := []byte{2, 2, typ, 1, 0x7f, 0x03, 0x40, 0x20, 0, 0x41, size, 0x6b, load, 0, 0, 0x21, 7,
		0x20, 3, 0x20, 4, 0x6a, 0x21, 3,
		0x20, 3, 0x41, 0, 0x20, 5, 0x20, 3, 0x20, 5, 0x48, 0x1b, 0x6b, 0x21, 3,
		0x20, 2, 0x20, 3, 0x41, shift, 0x74, 0x6a, 0x21, 9,
		0x20, 9, load, 0, size, 0x21, 8,
		0x20, 6, 0x20, 8, store, 0, 0,
		0x20, 0, load, 0, 0, 0x21, 8,
		0x20, 6, 0x20, 7, store, 0, size,
		0x20, 6, 0x20, 8, store, 0, 2 * size,
		0x20, 0, 0x41, 16, 0x6a, 0x21, 0,
		0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b,
		0x41, 0, 0x20, 0, 0x36, 0, 0, 0x41, 4, 0x20, 1, 0x36, 0, 0, 0x41, 8, 0x20, 3, 0x36, 0, 0, 0x41, 0, 0x0b}
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func TestCyclicLoadSpanMemoryAndPartialTraps(t *testing.T) {
	old := commonBoundsLimitEnabled
	commonBoundsLimitEnabled = false
	defer func() { commonBoundsLimitEnabled = old }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*17 + 9)
	}
	cases := [][7]uint64{
		{64, 1, 1024, 0, 1, 7, 2048}, {64, 7, 1024, 6, 1, 7, 2048}, {64, 7, 1024, 0, 0, 7, 2048}, {64, 7, 1024, 0, 7, 7, 2048},
		{64, 7, 1024, 0, 1, 7, 64}, {64, 7, 1024, 0, 1, 7, 1024}, {64, 7, 1024, 0, 1, 7, 1032},
		{65520, 7, 1024, 0, 1, 7, 2048}, {64, 7, 65520, 0, 1, 7, 2048}, {64, 7, 1024, 0, 1, 7, 65532},
		{64, 1, 1024, 0, 1, 0, 2048}, {64, 7, 1024, 7, 1, 7, 2048}, {64, 7, 1024, 0, 8, 7, 2048},
		{64, 1, 1024, 0x7fffffff, 1, 0x80000000, 2048}, {64, 1, 1024, 0xffffffff, 1, 7, 2048},
		{64, 1, 0xfffffff0, 0, 1, 7, 2048}, {64, 0, 1024, 0, 1, 7, 2048}, {64, 0xffffffff, 1024, 0, 1, 7, 2048},
		{0, 1, 1024, 0, 1, 7, 2048}, {0xffffffff, 1, 1024, 0, 1, 7, 2048},
		{0x8877665500000040, 0x8877665500000007, 0x8877665500000400, 0x8877665500000000, 0x8877665500000001, 0x8877665500000007, 2048},
	}
	for _, wide := range []bool{false, true} {
		for _, shift := range []byte{0, 3, 4} {
			m := cyclicSpanMemoryFixture(t, wide, shift)
			size := uint64(4)
			if wide {
				size = 8
			}
			for _, on := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"linear-load-span": true, "cyclic-load-span": on, "leaf-scratch-memsize": false, "memory-min-bounds": false}}
					if diagnosticsEnabled {
						var stats ModuleStats
						opts.Stats = &stats
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						cm.CodeImage.Close()
						opts.Stats = nil
						if (stats.Funcs[0].Peephole["cyclic-load-span"] > 0) != on {
							t.Fatalf("wide%v shift%d on%v compact%v stats%v", wide, shift, on, compact, stats.Funcs[0].Peephole)
						}
					}
					for _, tc := range cases {
						want := append([]byte{}, initial...)
						input, count, base, phase, step, limit, out := uint32(tc[0]), uint32(tc[1]), uint32(tc[2]), uint32(tc[3]), uint32(tc[4]), uint32(tc[5]), uint64(uint32(tc[6]))
						traps := false
						for iter := 0; ; iter++ {
							if iter > 65536 {
								t.Fatal("oracle did not terminate")
							}
							addr := uint64(input - uint32(size))
							if addr+size > 65536 {
								traps = true
								break
							}
							real := append([]byte{}, want[addr:addr+size]...)
							phase += step
							if int32(phase) >= int32(limit) {
								phase -= limit
							}
							index := uint64(base + (phase << shift))
							if index+2*size > 65536 {
								traps = true
								break
							}
							table := append([]byte{}, want[index+size:index+2*size]...)
							if out+size > 65536 {
								traps = true
								break
							}
							copy(want[out:out+size], table)
							if uint64(input)+size > 65536 {
								traps = true
								break
							}
							imag := append([]byte{}, want[uint64(input):uint64(input)+size]...)
							if out+2*size > 65536 {
								traps = true
								break
							}
							copy(want[out+size:out+2*size], real)
							if out+3*size > 65536 {
								traps = true
								break
							}
							copy(want[out+2*size:out+3*size], imag)
							input += 16
							count--
							if count == 0 {
								break
							}
						}
						if !traps {
							binary.LittleEndian.PutUint32(want, input)
							binary.LittleEndian.PutUint32(want[4:], count)
							binary.LittleEndian.PutUint32(want[8:], phase)
						}
						got, err := runLinearSpanMemory(t, m, opts, initial, tc[:]...)
						if (err != nil) != traps || !bytes.Equal(got, want) {
							t.Fatalf("wide%v shift%d on%v compact%v args%x trap%v err%v equal%v", wide, shift, on, compact, tc, traps, err, bytes.Equal(got, want))
						}
					}
				}
			}
		}
	}
}
