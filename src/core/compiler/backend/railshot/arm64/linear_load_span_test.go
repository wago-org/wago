//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestLinearLoadSpanGuardNativeBoundaries(t *testing.T) {
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
	args, result, trap := ar.Alloc(24), ar.Alloc(8), ar.Alloc(rt.TrapBufferBytes)
	for _, stride := range []uint32{1, 2, 8, 16, 2048} {
		for _, lower := range []uint32{1, 4, 8, 15} {
			a := &enc.Asm{}
			a.Load64(X20, X0, 0)
			a.Load64(X21, X0, 8)
			a.Load64(X27, X0, 16)
			f := fn{a: a, memSizeReg: X27}
			g := linearLoadSpan{input: X20, counter: X21, address: X9, end: X10, stride: stride, lower: lower, extent: 8}
			failures := f.emitLinearSpanGuard(g)
			a.MovImm64(X9, 1)
			a.Store64(X9, X3, 0)
			a.Ret()
			fail := a.Len()
			a.MovImm64(X9, 0)
			a.Store64(X9, X3, 0)
			a.Ret()
			for _, at := range failures {
				if !a.PatchBranch19(at, fail) {
					t.Fatal("guard branch out of range")
				}
			}
			code, entry, err := rt.MapCode(a.B)
			if err != nil {
				t.Fatal(err)
			}
			for _, base := range []uint64{0, 1, 4, 8, 15, 65528, 65532, 0xfffffff0, 0xfffffff8, 0xfffffffc, 0xffffffff, 0x100000008, 0x8877665500000008} {
				for _, count := range []uint64{0, 1, 2, 7, 65536, 0x80000000, 0xffffffff, 0x8877665500000001} {
					for _, memory := range []uint64{0, 65536, 1 << 32} {
						want := base == uint64(uint32(base)) && uint32(count) != 0 && base >= uint64(lower) && base+(uint64(uint32(count))-1)*uint64(stride)+8 <= memory
						binary.LittleEndian.PutUint64(args, base)
						binary.LittleEndian.PutUint64(args[8:], count)
						binary.LittleEndian.PutUint64(args[16:], memory)
						err := eng.Call(entry, args, jm.LinearMemory(), trap, result)
						if err != nil || (binary.LittleEndian.Uint64(result) != 0) != want {
							t.Fatalf("stride%d lower%d base%x count%x memory%x got%d want%v err%v", stride, lower, base, count, memory, binary.LittleEndian.Uint64(result), want, err)
						}
					}
				}
			}
			rt.Unmap(code)
		}
	}
}

func linearLoadSpanFixture(t testing.TB, wide bool, lower, stride int32) *wasm.Module {
	typ, load, store, size := byte(0x7d), byte(0x2a), byte(0x38), byte(4)
	if wide {
		typ, load, store, size = 0x7c, 0x2b, 0x39, 8
	}
	b := []byte{1, 2, typ, 0x03, 0x40, 0x20, 0, 0x41}
	b = append(b, wasmtest.SLEB32(lower)...)
	b = append(b, 0x6b, load, 0, 0, 0x21, 3, 0x20, 0, load, 0, 0, 0x21, 4,
		0x20, 2, 0x20, 3, store, 0, 0, 0x20, 2, 0x20, 4, store, 0, size,
		0x20, 0, 0x41)
	b = append(b, wasmtest.SLEB32(stride)...)
	b = append(b, 0x6a, 0x21, 0, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b,
		0x41, 0, 0x20, 0, 0x36, 0, 0, 0x41, 4, 0x20, 1, 0x36, 0, 0, 0x41, 0, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func TestLinearLoadSpanMemoryAliasesAndPartialTraps(t *testing.T) {
	before := commonBoundsLimitEnabled
	commonBoundsLimitEnabled = false
	defer func() { commonBoundsLimitEnabled = before }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*17 + 9)
	}
	for _, wide := range []bool{false, true} {
		for _, stride := range []int32{1, 8, 16} {
			size := uint64(4)
			if wide {
				size = 8
			}
			lower := int32(size)
			m := linearLoadSpanFixture(t, wide, lower, stride)
			for _, on := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"linear-load-span": on, "leaf-scratch-memsize": false, "memory-min-bounds": false}}
					if diagnosticsEnabled {
						var stats ModuleStats
						opts.Stats = &stats
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						image := append([]byte{}, cm.Code...)
						cm.CodeImage.Close()
						if (stats.Funcs[0].Peephole["linear-load-span"] > 0) != on {
							t.Fatalf("wide%v stride%d on%v compact%v hits%v image%x", wide, stride, on, compact, stats.Funcs[0].Peephole, image)
						}
						opts.Stats = nil
					}
					for _, tc := range []struct{ base, count, out uint64 }{
						{16, 1, 128}, {16, 7, 128}, {16, 7, 16}, {16, 7, 8}, {16, 7, 24},
						{0, 1, 128}, {1, 1, 128}, {65520, 1, 128}, {65520, 7, 128}, {65528, 2, 128},
						{16, 2, 65532}, {16, 2, 65528}, {16, 0, 128}, {16, 0xffffffff, 128},
						{0xffffffff, 1, 128}, {0x8877665500000010, 0x8877665500000007, 128},
					} {
						want := append([]byte{}, initial...)
						ptr, count, out := uint32(tc.base), uint32(tc.count), uint64(uint32(tc.out))
						traps := false
						for steps := 0; ; steps++ {
							if steps > 65536 {
								t.Fatal("oracle failed to terminate")
							}
							real := uint64(ptr - uint32(lower))
							imag := uint64(ptr)
							if real+size > 65536 || imag+size > 65536 {
								traps = true
								break
							}
							high, low := append([]byte{}, want[real:real+size]...), append([]byte{}, want[imag:imag+size]...)
							if out+size > 65536 {
								traps = true
								break
							}
							copy(want[out:out+size], high)
							if out+2*size > 65536 {
								traps = true
								break
							}
							copy(want[out+size:out+2*size], low)
							ptr += uint32(stride)
							count--
							if count == 0 {
								break
							}
						}
						if !traps {
							binary.LittleEndian.PutUint32(want, ptr)
							binary.LittleEndian.PutUint32(want[4:], count)
						}
						got, err := runLinearSpanMemory(t, m, opts, initial, tc.base, tc.count, tc.out)
						if (err != nil) != traps || !bytes.Equal(got, want) {
							t.Fatalf("wide%v stride%d on%v compact%v base%x count%x out%x trap%v err%v equal%v", wide, stride, on, compact, tc.base, tc.count, tc.out, traps, err, bytes.Equal(got, want))
						}
					}
				}
			}
		}
	}
}

func TestLinearLoadSpanNativeRejections(t *testing.T) {
	a := &enc.Asm{}
	a.SubImm32(X9, X20, 4)
	a.AddImm64(X10, X9, 4)
	a.CmpReg64(X10, X27)
	first := a.Bcond(condA)
	a.LdrFIdx(8, linMemReg, X9, 0, false)
	a.AddImm64(X9, X20, 4)
	a.CmpReg64(X9, X27)
	second := a.Bcond(condA)
	a.LdrFIdx(9, linMemReg, X20, 0, false)
	a.AddImm32(X20, X20, 8)
	a.SubsImm32(X21, X21, 1)
	back := a.Bcond(condNE)
	trap := a.Len()
	a.Ret()
	a.PatchBranch19(first, trap)
	a.PatchBranch19(second, trap)
	a.PatchBranch19(back, 0)
	orig := append([]byte{}, a.B...)
	for _, tc := range []struct {
		name string
		pc   int
		word uint32
		want bool
	}{
		{"accepted", 16, rdWord(orig, 16), true},
		{"unknown call", 16, 0x94000000, false},
		{"input overwritten", 16, 0x11000694, false},
		{"counter overwritten", 16, 0x110006b5, false},
		{"memory size overwritten", 16, 0x1100077b, false},
		{"flags from omitted check read", 16, 0x1a9f17e8, false},
		{"wrong first extent", 4, 0x9100212a, false},
		{"non power stride", 36, 0x11000e94, false},
		{"wrong counter step", 40, 0x71000ab5, false},
		{"ascending first address", 0, 0x11001289, false},
		{"different second load index", 32, 0xbc756b49, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte{}, orig...)
			wrWord(b, tc.pc, tc.word)
			_, ok := matchLinearLoadSpan(b, back, X27, nil, nil)
			if ok != tc.want {
				t.Fatalf("got%v want%v", ok, tc.want)
			}
		})
	}
	for at := 4; at <= back; at += 4 {
		targets := make([]uint64, 1)
		branchTargetAdd(targets, at, len(orig))
		if _, ok := matchLinearLoadSpan(orig, back, X27, targets, nil); ok {
			t.Fatalf("foreign branch entry at%d", at)
		}
		if _, ok := matchLinearLoadSpan(orig, back, X27, nil, []int{at}); ok {
			t.Fatalf("alternate entry at%d", at)
		}
	}
}

func TestLinearLoadSpanEndValueLiveness(t *testing.T) {
	for _, tc := range []struct {
		name string
		body func(*enc.Asm)
		exit func(*enc.Asm) []callReloc
		want bool
	}{
		{"caller scratch return", nil, func(a *enc.Asm) []callReloc { a.Ret(); return nil }, true},
		{"visible result", nil, func(a *enc.Asm) []callReloc { a.MovReg64(X0, X10); a.Ret(); return nil }, false},
		{"stored end value", nil, func(a *enc.Asm) []callReloc { a.Store64(X10, linMemReg, 0); a.Ret(); return nil }, false},
		{"end used as address", nil, func(a *enc.Asm) []callReloc { a.Load64(X0, X10, 0); a.Ret(); return nil }, false},
		{"float store address", nil, func(a *enc.Asm) []callReloc { a.StrF(X10, 0, 0, false); a.Ret(); return nil }, false},
		{"defined before use", nil, func(a *enc.Asm) []callReloc { a.Load64(X10, SP, 0); a.MovReg64(X0, X10); a.Ret(); return nil }, true},
		{"body observes end", func(a *enc.Asm) { a.Add32(X11, X10, X12) }, func(a *enc.Asm) []callReloc { a.Ret(); return nil }, false},
		{"body observes retained address", func(a *enc.Asm) { a.Add32(X11, X9, X12) }, func(a *enc.Asm) []callReloc { a.Ret(); return nil }, true},
		{"one path observes end", nil, func(a *enc.Asm) []callReloc {
			b := a.Cbnz32(X1)
			a.MovImm64(X10, 0)
			a.Ret()
			alt := a.Len()
			a.MovReg64(X0, X10)
			a.Ret()
			a.PatchBranch19(b, alt)
			return nil
		}, false},
		{"unresolved tail relocation", nil, func(a *enc.Asm) []callReloc { at := a.Branch(); return []callReloc{{at: uint32(at)}} }, false},
		{"killed before an unknown call", nil, func(a *enc.Asm) []callReloc { a.MovImm64(X9, 0); a.MovImm64(X10, 0); a.Blr(X0); a.Ret(); return nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &enc.Asm{}
			a.SubImm32(X9, X20, 4)
			a.AddImm64(X10, X9, 4)
			a.CmpReg64(X10, X27)
			one := a.Bcond(condA)
			a.LdrFIdx(8, linMemReg, X9, 0, false)
			if tc.body != nil {
				tc.body(a)
			}
			second := a.Len()
			a.AddImm64(X9, X20, 4)
			a.CmpReg64(X9, X27)
			two := a.Bcond(condA)
			a.LdrFIdx(9, linMemReg, X20, 0, false)
			a.AddImm32(X20, X20, 8)
			a.SubsImm32(X21, X21, 1)
			back := a.Bcond(condNE)
			relocs := tc.exit(a)
			trap := a.Len()
			a.Ret()
			a.PatchBranch19(one, trap)
			a.PatchBranch19(two, trap)
			a.PatchBranch19(back, 0)
			g, ok := matchLinearLoadSpan(a.B, back, X27, nil, nil)
			if !ok {
				t.Fatal("test loop did not match")
			}
			if g.second != second {
				t.Fatal("wrong second check")
			}
			if got := spanEndsDead(a.B, g, relocs); got != tc.want {
				t.Fatalf("got%v want%v", got, tc.want)
			}
		})
	}
	// Fixed ABI homes and result registers cannot be discarded merely at RET.
	a := &enc.Asm{}
	a.Ret()
	for _, reg := range []Reg{X0, X3, X16, X17, X19, X26, X27} {
		if spanExitClobbersDead(a.B, 0, uint32(1)<<uint32(reg), nil) {
			t.Fatalf("discarded ABI home%d", reg)
		}
	}
}
