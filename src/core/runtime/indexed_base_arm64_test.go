//go:build (linux || darwin || windows) && arm64

package runtime

import (
	"encoding/binary"
	"fmt"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Execute the encoder sequence independently of register-allocation choices.
// Base/index aliases must use the new offset; an X16 destination must force
// reconstruction; an independent destination must retain the original address.
func TestSignedLoadIndexedBaseExecution(t *testing.T) {
	for _, size := range []int{1, 2, 4} {
		for _, alias := range []string{"base", "index", "scratch", "independent"} {
			for _, stable := range []bool{false, true} {
				for _, reuse := range []bool{false, true} {
					t.Run(fmt.Sprintf("size%d/%s/stable=%t/reuse=%t", size, alias, stable, reuse), func(t *testing.T) {
						eng, jm, ar := fixture(t)
						mem := jm.CurrentBytes()
						binary.LittleEndian.PutUint32(mem[4:], 16)
						binary.LittleEndian.PutUint32(mem[8:], 111)
						binary.LittleEndian.PutUint32(mem[24:], 222)
						a := a64.Asm{DenseIdxDisp: true, ReuseIndexedBase: reuse}
						base, index, dst := a64.X4, a64.X5, a64.X7
						a.MovReg64(base, a64.X1) // runtime ABI: X1 = linear memory
						a.MovImm64(index, 0)
						want := uint32(111)
						switch alias {
						case "base":
							// Put the small offset in the base operand so the
							// signed load can replace it without truncating a pointer.
							base, index = index, base
							dst, want = base, 222
						case "index":
							dst, want = index, 222
						case "scratch":
							dst = a64.X16
						}
						a.LoadIdx(dst, base, index, 4, size, true, true)
						if stable {
							a.Add32(a64.X8, a64.X8, a64.X9)
						}
						a.LoadIdx(a64.X6, base, index, 8, 4, false, false)
						must(a.Store32(a64.X6, a64.X3, 0))
						must(a.Store32(a64.XZR, a64.X2, 0))
						a.Ret()
						code, entry, err := MapCode(a.B)
						if err != nil {
							t.Fatal(err)
						}
						defer Unmap(code)
						args, results, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(TrapBufferBytes)
						if err := eng.Call(entry, args, jm.LinearMemory(), trap, results); err != nil {
							t.Fatal(err)
						}
						if got := binary.LittleEndian.Uint32(results); got != want {
							t.Fatalf("loaded %d, want %d", got, want)
						}
					})
				}
			}
		}
	}
}
