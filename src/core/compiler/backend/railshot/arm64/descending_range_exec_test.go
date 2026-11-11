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

func descendingRangeFixture() *enc.Asm {
	a := &enc.Asm{}
	a.MovReg64(linMemReg, X1)
	a.MovImm64(X25, 65532)
	a.Load64(X20, X0, 0)
	a.Load64(X19, X0, 8)
	a.Load64(X22, X0, 16)
	start := a.Len()
	a.CmpReg64(X20, X25)
	trap1 := a.Bcond(condA)
	a.B = binary.LittleEndian.AppendUint32(a.B, 0xb8746b55)
	a.CmpReg32(X21, X22)
	early := a.Bcond(condBE)
	a.AddImm32(X11, X20, 4)
	a.CmpReg64(X11, X25)
	trap2 := a.Bcond(condA)
	a.B = binary.LittleEndian.AppendUint32(a.B, 0xb82b6b55)
	a.SubImm32(X20, X20, 4)
	a.SubsImm32(X19, X19, 1)
	back := a.Bcond(condNE)
	a.PatchBranch19(back, start)
	exit := a.Len()
	a.MovImm64(X11, 0)
	a.Store64(X11, X3, 0)
	a.Store64(X19, X3, 8)
	a.Store64(X20, X3, 16)
	a.Ret()
	trap := a.Len()
	a.MovImm64(X11, 1)
	a.Store64(X11, X3, 0)
	a.Store64(X19, X3, 8)
	a.Store64(X20, X3, 16)
	a.Ret()
	a.PatchBranch19(early, exit)
	a.PatchBranch19(trap1, trap)
	a.PatchBranch19(trap2, trap)
	return a
}

func TestDescendingRangeExactMemoryAndFallback(t *testing.T) {
	old := descendingRangeEnabled
	descendingRangeEnabled = true
	t.Cleanup(func() { descendingRangeEnabled = old })
	original := descendingRangeFixture()
	optimized := descendingRangeFixture()
	f := gpClassEmitter()
	f.a = optimized
	f.m = &wasm.Module{}
	f.hasLoop = true
	f.descendingRangeCandidate = true
	f.memLimitReg = X25
	f.memLimitExtent = 4
	f.policy = currentCodegenPolicy()
	before := optimized.Len()
	f.finalizeDescendingMemoryRange()
	if optimized.Len() == before {
		t.Fatal("guarded path not emitted")
	}
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
	args, result, trap := ar.Alloc(32), ar.Alloc(32), ar.Alloc(rt.TrapBufferBytes)
	initial := make([]byte, 65536)
	for i := 0; i < len(initial)/4; i++ {
		binary.LittleEndian.PutUint32(initial[i*4:], uint32(i%101))
	}
	cases := [][3]uint64{{128, 1, 0}, {128, 12, 0}, {128, 12, 100}, {0, 1, 0}, {0, 2, 0}, {65528, 2, 0}, {65532, 1, 0}, {65535, 1, 0}, {65536, 1, 0}, {0xffffffff, 1, 0}, {0x100000080, 1, 0}, {128, 0, 100}, {128, 0xffffffff, 100}}
	for _, p := range []uint64{0, 1, 4, 8, 16, 32, 64, 65524, 65528, 65532, 65533} {
		for _, count := range []uint64{1, 2, 3, 8, 17} {
			for _, key := range []uint64{0, 10, 100} {
				cases = append(cases, [3]uint64{p, count, key})
			}
		}
	}
	for _, c := range cases {
		var wantMem, wantRes []byte
		for i, image := range [][]byte{original.B, optimized.B} {
			copy(jm.CurrentBytes(), initial)
			clear(result)
			clear(trap)
			for k, v := range c {
				binary.LittleEndian.PutUint64(args[8*k:], v)
			}
			code, entry, e := rt.MapCode(image)
			if e != nil {
				t.Fatal(e)
			}
			e = eng.Call(entry, args, jm.LinearMemory(), trap, result)
			rt.Unmap(code)
			if e != nil {
				t.Fatalf("case=%v image=%d error=%v", c, i, e)
			}
			if i == 0 {
				wantMem = append([]byte(nil), jm.CurrentBytes()...)
				wantRes = append([]byte(nil), result...)
			} else if !bytes.Equal(wantMem, jm.CurrentBytes()) || !bytes.Equal(wantRes, result) {
				t.Fatalf("case=%v optimized differs got=%x want=%x", c, result, wantRes)
			}
		}
	}
}
