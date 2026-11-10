//go:build (linux || darwin || windows) && arm64

package runtime

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestVectorLargeStackDisplacement(t *testing.T) {
	for _, store := range []bool{false, true} {
		name := "load"
		if store {
			name = "store"
		}
		t.Run(name, func(t *testing.T) {
			eng, jm, ar := fixture(t)
			var a a64.Asm
			a.SubImm64LSL12(a64.SP, a64.SP, 0x5000)
			a.AddImm64LSL12(a64.X5, a64.SP, 0x4000)
			// Use scalar accesses to establish independent high and low slot oracles.
			for lane := 0; lane < 4; lane++ {
				a.MovImm64(a64.X4, uint64(101+lane*12))
				must(a.Store32(a64.X4, a64.SP, uint32(0xf8+lane*4)))
				a.MovImm64(a64.X4, uint64(71+lane*10))
				must(a.Store32(a64.X4, a64.X5, uint32(0xf8+lane*4)))
			}
			if store {
				a.LdrQ(a64.X17, a64.X0, 0)
				a.VMovdquStoreDisp(a64.SP, 0x40f8, a64.X17)
				// Read the intended slot without using the Q fallback under test.
				a.AddImm64LSL12(a64.X5, a64.SP, 0x4000)
				a.AddImm64(a64.X5, a64.X5, 0xf8)
				a.LdrQ(a64.X16, a64.X5, 0)
			} else {
				a.VMovdquLoadDisp(a64.X16, a64.SP, 0x40f8)
			}
			for lane := byte(0); lane < 4; lane++ {
				a.NeonUmovS(a64.X4, a64.X16, lane)
				must(a.Store32(a64.X4, a64.X3, uint32(lane)*4))
				must(a.Load32(a64.X4, a64.SP, 0xf8+uint32(lane)*4))
				must(a.Store32(a64.X4, a64.X3, 16+uint32(lane)*4))
			}
			a.AddImm64LSL12(a64.SP, a64.SP, 0x5000)
			must(a.Store32(a64.ZR, a64.X2, 0))
			a.Ret()
			code, err := mmapExec(a.B)
			if err != nil {
				t.Fatal(err)
			}
			defer munmap(code)
			args, results, trap := ar.Alloc(16), ar.Alloc(32), ar.Alloc(TrapBufferBytes)
			lanes := [4]uint32{17, 29, 43, 61}
			for i, v := range lanes {
				binary.LittleEndian.PutUint32(args[i*4:], v)
			}
			if err := eng.Call(slicePtr(code), args, jm.LinearMemory(), trap, results); err != nil {
				t.Fatal(err)
			}
			for i := range lanes {
				want := uint32(71 + i*10)
				if store {
					want = lanes[i]
				}
				if got := binary.LittleEndian.Uint32(results[i*4:]); got != want {
					t.Errorf("high slot lane %d = %d, want %d", i, got, want)
				}
				if got, want := binary.LittleEndian.Uint32(results[16+i*4:]), uint32(101+i*12); got != want {
					t.Errorf("low slot lane %d = %d, want %d", i, got, want)
				}
			}
		})
	}
}
