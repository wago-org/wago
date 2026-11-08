//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"math"
	"testing"
)

func affineBoundsFixture() (*enc.Asm, int, int) {
	a := &enc.Asm{}
	a.SubSP64(16)
	a.MovReg64(linMemReg, X1)
	a.MovImm64(X27, 65536)
	a.Load64(X23, X0, 0)
	a.Load64(X24, X0, 8)
	a.Load64(X9, X0, 16)
	a.Store64(X9, SP, 0)
	a.Load64(X20, X0, 24)
	var checks []int
	check := func(address, end Reg) {
		a.AddImm64(end, address, 8)
		a.CmpReg64(end, X27)
		checks = append(checks, a.Bcond(condA))
	}
	word := func(w uint32) { a.B = binary.LittleEndian.AppendUint32(a.B, w) }
	load := func(v, address Reg) { word(0xfc606800 | uint32(address)<<16 | uint32(linMemReg)<<5 | uint32(v)) }
	store := func(v, address Reg) { word(0xfc206800 | uint32(address)<<16 | uint32(linMemReg)<<5 | uint32(v)) }
	start := a.Len()
	a.Load64(X22, SP, 0)
	a.Add32(X22, X22, X20)
	a.Nop()
	check(X23, X9)
	a.MovReg32(X25, X24)
	check(X25, X9)
	load(0, X23)
	load(1, X25)
	a.Fmul(0, 0, 1, true)
	check(X22, X9)
	load(1, X22)
	a.Fadd(0, 0, 1, true)
	store(0, X22)
	a.AddImm32(X19, X22, 8)
	a.AddImm32(X9, X25, 24)
	check(X9, X10)
	load(0, X23)
	load(1, X9)
	a.Fmul(0, 0, 1, true)
	check(X19, X9)
	load(1, X19)
	a.Fadd(0, 0, 1, true)
	store(0, X19)
	a.AddImm32(X24, X24, 48)
	a.AddImm32(X20, X20, 16)
	a.CmpImm32(X20, 320)
	back := a.Bcond(condNE)
	a.PatchBranch19(back, start)
	a.MovImm64(X9, 0)
	a.Store64(X9, X3, 0)
	a.Store64(X20, X3, 8)
	a.AddSP64(16)
	a.Ret()
	trap := a.Len()
	a.MovImm64(X9, 1)
	a.Store64(X9, X3, 0)
	a.Store64(X20, X3, 8)
	a.AddSP64(16)
	a.Ret()
	for _, pc := range checks {
		a.PatchBranch19(pc, trap)
	}
	return a, start, back
}

func TestAffineBoundsLoopProof(t *testing.T) {
	a, start, back := affineBoundsFixture()
	p, ok := matchAffineBoundsLoop(a.B, back, X27, nativeConstantTargets(a.B), nil, nil)
	if !ok || p.start != start || p.iterations != 20 || p.streamN != 3 || p.omitN != 10 || p.guardA == regNone || p.guardB == regNone {
		t.Fatalf("plan%+v ok%v", p, ok)
	}
	// A frame write invalidates every frame-derived stream. An internal entry
	// bypasses entry guards. Flag-consuming or unrecognized instructions decline.
	for _, w := range []uint32{0xf90003f6, 0x9a890129, 0xd63f0200, 0x910003ff} {
		b := append([]byte{}, a.B...)
		wrWord(b, start, w)
		if _, ok := matchAffineBoundsLoop(b, back, X27, nativeConstantTargets(b), nil, nil); ok {
			t.Fatalf("admitted unexpected first word%x", w)
		}
	}
	for _, reg := range []Reg{linMemReg, X27} {
		b := append([]byte{}, a.B...)
		wrWord(b, start+8, 0xf94003e0|uint32(reg)) // reload into a fixed memory role
		if _, ok := matchAffineBoundsLoop(b, back, X27, nativeConstantTargets(b), nil, nil); ok {
			t.Fatalf("admitted write to fixed memory role%d", reg)
		}
	}
	if _, ok := matchAffineBoundsLoop(a.B, back, X27, nativeConstantTargets(a.B), []int{start + 4}, nil); ok {
		t.Fatal("admitted internal entry")
	}
	b := append([]byte{}, a.B...)
	wrWord(b, back-8, 0x11006294) // counter += 24; 320 is not divisible.
	if _, ok := matchAffineBoundsLoop(b, back, X27, nativeConstantTargets(b), nil, nil); ok {
		t.Fatal("admitted unreachable fixed bound")
	}
}

func TestAffineBoundsLoopExactMemoryAndFallback(t *testing.T) {
	if !affineBoundsLoopEnabled {
		t.Skip("affine bounds loop disabled")
	}
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
	args, result, trap := ar.Alloc(32), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
	images := make([][]byte, 2)
	original, _, _ := affineBoundsFixture()
	images[0] = original.B
	optimized, _, _ := affineBoundsFixture()
	f := gpClassEmitter()
	f.a = optimized
	f.m = &wasm.Module{}
	f.hasLoop = true
	f.memSizeReg = X27
	f.policy = currentCodegenPolicy()
	before := optimized.Len()
	f.finalizeAffineBoundsLoops()
	if optimized.Len() <= before {
		t.Fatal("fixture did not emit guarded clone")
	}
	images[1] = optimized.B
	initial := make([]byte, 65536)
	for i := 0; i < len(initial)/8; i++ {
		binary.LittleEndian.PutUint64(initial[8*i:], math.Float64bits(float64(i%17-8)*0.125))
	}
	special := append([]byte{}, initial...)
	for i, v := range []uint64{0x7ff8000000000000, 0xfff8000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x8000000000000000, 1, 0x000fffffffffffff} {
		binary.LittleEndian.PutUint64(special[32+8*i:], v)
	}
	cases := [][4]uint64{
		{32, 256, 8192, 0}, {32, 256, 8192, 16}, {32, 256, 8192, 304},
		{32, 256, 256, 0}, {32, 256, 264, 0}, {8192, 256, 8192, 0},
		{65532, 256, 8192, 0}, {65528, 65512, 8192, 0}, {32, 256, 65512, 0},
		{32, 65528, 8192, 0}, {32, 65500, 8192, 0}, {32, 256, 65528, 0},
		{32, 256, 0xfffffff0, 0}, {32, 0xfffffff0, 8192, 0},
		{32, 256, 0xfffffff0, 0xfffffff0}, {32, 256, 8192, 1},
		{0x100000020, 256, 8192, 0},
	}
	for _, memory := range [][]byte{initial, special} {
		for _, c := range cases {
			var wantMemory, wantResult []byte
			for k, image := range images {
				t.Logf("case=%v image=%d", c, k)
				copy(jm.CurrentBytes(), memory)
				clear(result)
				clear(trap)
				for i, v := range c {
					binary.LittleEndian.PutUint64(args[8*i:], v)
				}
				code, entry, e := rt.MapCode(image)
				if e != nil {
					t.Fatal(e)
				}
				e = eng.Call(entry, args, jm.LinearMemory(), trap, result)
				rt.Unmap(code)
				if e != nil {
					t.Fatalf("case%v image%d native error%v", c, k, e)
				}
				if k == 0 {
					wantMemory = append([]byte{}, jm.CurrentBytes()...)
					wantResult = append([]byte{}, result...)
				} else if !bytes.Equal(wantMemory, jm.CurrentBytes()) || !bytes.Equal(wantResult, result) {
					t.Fatalf("case%v optimized memory/continuation differs: got%x want%x", c, result, wantResult)
				}
			}
		}
	}
}

func TestAffineBoundsLoopProbeAllocatesZero(t *testing.T) {
	a, _, back := affineBoundsFixture()
	targets := nativeConstantTargets(a.B)
	allocs := testing.AllocsPerRun(100, func() {
		if _, ok := matchAffineBoundsLoop(a.B, back, X27, targets, nil, nil); !ok {
			panic("eligible bounded probe rejected")
		}
	})
	if allocs != 0 {
		t.Fatalf("bounded probe allocated%v times", allocs)
	}
}

func TestAffineBoundsLoopPerCompilationPolicy(t *testing.T) {
	for _, on := range []bool{false, true} {
		a, _, _ := affineBoundsFixture()
		before := a.Len()
		selection, err := optimizationBindings.ResolveSnapshot(map[string]bool{"affine-bounds-loop": on}, OptimizationSnapshot{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		f := gpClassEmitter()
		f.a = a
		f.m = &wasm.Module{}
		f.hasLoop = true
		f.memSizeReg = X27
		f.policy = shared.DefaultCodegenPolicy(selection)
		f.finalizeAffineBoundsLoops()
		if (a.Len() > before) != on {
			t.Fatalf("on%v output grew from%d to%d", on, before, a.Len())
		}
	}
}
