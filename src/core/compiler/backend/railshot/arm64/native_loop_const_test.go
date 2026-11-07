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

func TestNativeLoopConstantMaterialization(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, value := range []uint64{0x12345678, 0x123456789abcdef1, 0xffff1234ffff5678} {
			a := &enc.Asm{}
			if wide {
				a.MovImm64(X12, value)
			} else {
				a.MovImm32(X12, int32(value))
				value = uint64(uint32(value))
			}
			c, ok := nativeLoopConstAt(a.B, 0, a.Len())
			if ok != (a.Len() >= 8) || c.value != value || c.reg != X12 || c.wide != wide || c.words*4 != a.Len() {
				t.Fatalf("wide%v value%x got%+v ok%v", wide, value, c, ok)
			}
		}
	}
	for _, words := range [][]uint32{
		{0xf2a0000c},             // MOVK alone observes the incoming value.
		{0xd280002c},             // One cheap move cannot repay a lease.
		{0xd280003f, 0xf2a0003f}, // ZR is not storage.
		{0x5280000c, 0x72c0000c}, // Invalid W MOVK high halfword.
	} {
		a := &enc.Asm{}
		for _, w := range words {
			a.B = append(a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
		}
		if _, ok := nativeLoopConstAt(a.B, 0, a.Len()); ok {
			t.Fatalf("admitted %x", words)
		}
	}
}

func nativeConstantFixture(wide bool) (*enc.Asm, int, int) {
	a := &enc.Asm{}
	a.Load64(X20, X0, 0)
	a.Load64(X21, X0, 8)
	for _, reg := range []Reg{X9, X10, X11, X14} {
		a.MovImm64(reg, 0)
	}
	start := a.Len()
	for i := 0; i < 4; i++ {
		reg := X12
		if i&1 != 0 {
			reg = X13
		}
		if wide {
			a.MovImm64(reg, 0x123456789abcdef1)
			a.Add64(X21, X21, reg)
		} else {
			a.MovImm32(reg, 0x12345678)
			a.Add32(X21, X21, reg)
		}
	}
	a.SubsImm32(X20, X20, 1)
	back := a.Bcond(condNE)
	a.PatchBranch19(back, start)
	for range 6 {
		a.Nop()
	}
	a.Store64(X21, X3, 0)
	a.Ret()
	return a, start, back
}

func nativeConstantTargets(b []byte) []uint64 {
	bits := make([]uint64, ((len(b)/4)+63)/64)
	for at := 0; at < len(b); at += 4 {
		if to, ok := branchTarget(at, rdWord(b, at)); ok {
			branchTargetAdd(bits, to, len(b))
		}
	}
	return bits
}

func TestNativeLoopConstantProof(t *testing.T) {
	a, start, back := nativeConstantFixture(true)
	p, ok := matchNativeLoopConst(a.B, back, nativeConstantTargets(a.B), nil, nil)
	if !ok || p.start != start || p.cache != X9 || len(p.uses) != 4 {
		t.Fatalf("plan%+v ok%v", p, ok)
	}
	for _, tc := range []struct {
		name string
		pc   int
		word uint32
	}{
		{"unknown", start + 16, 0xffffffff}, {"call", start + 16, 0x94000000}, {"indirect", start + 16, 0xd61f0120},
		{"interior edge", start + 16, 0x54000001}, {"writeback load", start + 16, 0xf8408c09}, {"predicate", back, rdWord(a.B, back) ^ 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte{}, a.B...)
			wrWord(b, tc.pc, tc.word)
			if _, ok := matchNativeLoopConst(b, back, nativeConstantTargets(b), nil, nil); ok {
				t.Fatal("invalid loop admitted")
			}
		})
	}
	if _, ok := matchNativeLoopConst(a.B, back, nativeConstantTargets(a.B), []int{start + 4}, nil); ok {
		t.Fatal("foreign entry admitted")
	}
	b := append([]byte{}, a.B...)
	for i, reg := range []Reg{X9, X10, X11, X14, X13, X12} {
		wrWord(b, back+4+i*4, 0xf9000340|uint32(reg))
	}
	if _, ok := matchNativeLoopConst(b, back, nativeConstantTargets(b), nil, nil); ok {
		t.Fatal("observed live-out registers leased")
	}
	cheap, _, cheapBack := nativeConstantFixture(false)
	if _, ok := matchNativeLoopConst(cheap.B, cheapBack, nativeConstantTargets(cheap.B), nil, nil); ok {
		t.Fatal("cheap two-word materializations leased")
	}
	// An integer load's destination/address/index, and a store's actual data,
	// all exclude the corresponding register from the lease.
	for _, w := range []uint32{0xf8696b4a, 0xf8296b4a, 0xf94003e9, 0xf90003e9, 0x9bca7d29} {
		x, ok := nativeLoopConstWord(w)
		if !ok || (x.reads|x.writes)&(1<<9) == 0 {
			t.Fatalf("missing operand effect %x %+v %v", w, x, ok)
		}
	}
}

func TestNativeLoopConstantNativeExecution(t *testing.T) {
	eng, e := rt.NewEngine()
	if e != nil {
		t.Fatal(e)
	}
	defer eng.Close()
	jm, e := rt.NewJobMemory(65536)
	if e != nil {
		t.Fatal(e)
	}
	defer jm.Close()
	ar, e := rt.NewArena(4096)
	if e != nil {
		t.Fatal(e)
	}
	defer ar.Close()
	args, result, trap := ar.Alloc(32), ar.Alloc(8), ar.Alloc(rt.TrapBufferBytes)
	old := nativeLoopConstLayoutOnly
	defer func() { nativeLoopConstLayoutOnly = old }()
	for _, wide := range []bool{false, true} {
		for _, layout := range []bool{false, true} {
			a, _, _ := nativeConstantFixture(wide)
			before := a.Len()
			f := gpClassEmitter()
			f.a = a
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{}
			f.hasLoop = true
			nativeLoopConstLayoutOnly = layout
			f.finalizeNativeLoopConsts()
			if (a.Len() > before) != wide {
				t.Fatalf("wide%v layout%v did not activate", wide, layout)
			}
			code, entry, e := rt.MapCode(a.B)
			if e != nil {
				t.Fatal(e)
			}
			for _, n := range []uint32{1, 2, 17, 128} {
				for _, seed := range []uint64{0, 1, 0x8877665544332211, ^uint64(0)} {
					want := seed
					for i := uint32(0); i < n; i++ {
						for range 4 {
							if wide {
								want += 0x123456789abcdef1
							} else {
								want = uint64(uint32(want) + 0x12345678)
							}
						}
					}
					binary.LittleEndian.PutUint64(args, uint64(n))
					binary.LittleEndian.PutUint64(args[8:], seed)
					e := eng.Call(entry, args, jm.LinearMemory(), trap, result)
					if e != nil || binary.LittleEndian.Uint64(result) != want {
						t.Fatalf("wide%v layout%v n%d seed%x result%x want%x err%v", wide, layout, n, seed, binary.LittleEndian.Uint64(result), want, e)
					}
				}
			}
			rt.Unmap(code)
		}
	}
}

func nativeConstantMemoryFixture(t testing.TB) *wasm.Module {
	const c = int64(0x23456789abcdef13)
	b := []byte{1, 1, 0x7e, 0x41, 8, 0x20, 0, 0x29, 0, 0, 0x20, 0, 0x29, 0, 0, 0x85, 0x37, 0, 0, 0x42, 7, 0x21, 2, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x29, 0, 0, 0x85, 0x42}
	b = append(b, wasmtest.SLEB64(c)...)
	b = append(b, 0x7e, 0x42)
	b = append(b, wasmtest.SLEB64(c)...)
	b = append(b, 0x85, 0x21, 2, 0x20, 0, 0x20, 2, 0x37, 0, 0, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x41, 0, 0x20, 2, 0x37, 0, 0, 0x20, 2, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, b)
}

func TestNativeLoopConstantMemoryAndTraps(t *testing.T) {
	m := nativeConstantMemoryFixture(t)
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*71 + 19)
	}
	for _, compact := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"native-loop-const": on, "loop-int-const": false, "scoped-loop-int-const": false, "zero-branch": false}}
			if diagnosticsEnabled {
				var stats ModuleStats
				opts.Stats = &stats
				cm, e := CompileModuleWith(m, opts)
				if e != nil {
					t.Fatal(e)
				}
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
				opts.Stats = nil
				if (stats.Funcs[0].Peephole["native-loop-constant-lease"] > 0) != on {
					t.Fatalf("on%v compact%v stats%v", on, compact, stats.Funcs[0].Peephole)
				}
			}
			for _, tc := range [][2]uint64{{16, 1}, {16, 17}, {65528, 1}, {65528, 2}, {65532, 1}, {16, 0}, {0xffffffff, 1}, {0xaabbccdd00000010, 0x1122334400000003}} {
				want := append([]byte{}, initial...)
				p, n := uint32(tc[0]), uint32(tc[1])
				hash := uint64(7)
				traps := uint64(p)+8 > 65536
				if !traps {
					binary.LittleEndian.PutUint64(want[8:], 0)
				}
				for i := 0; ; i++ {
					if i > 8192 {
						t.Fatal("oracle did not terminate")
					}
					if traps || uint64(p)+8 > 65536 {
						traps = true
						break
					}
					hash = ((hash ^ binary.LittleEndian.Uint64(want[uint64(p):])) * 0x23456789abcdef13) ^ 0x23456789abcdef13
					binary.LittleEndian.PutUint64(want[uint64(p):], hash)
					p += 8
					n--
					if n == 0 {
						break
					}
				}
				if !traps {
					binary.LittleEndian.PutUint64(want, hash)
				}
				got, e := runLinearSpanMemory(t, m, opts, initial, tc[:]...)
				if (e != nil) != traps || !bytes.Equal(got, want) {
					t.Fatalf("on%v compact%v args%x trap%v err%v equal%v", on, compact, tc, traps, e, bytes.Equal(got, want))
				}
			}
		}
	}
}
