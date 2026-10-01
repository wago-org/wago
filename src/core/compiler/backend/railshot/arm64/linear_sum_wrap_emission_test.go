//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestLinearSumWrappingGroupsUseScalarTailARM64(t *testing.T) {
	// Inspect emission only: start=fffffff0/count=5 needs per-load i32 wrap
	// after the scalar peel, rather than native offsets beyond a 4 GiB memory.
	for _, tc := range []struct {
		name   string
		hasMax bool
		max    uint64
		wrap   bool
	}{{"unbounded", false, 0, true}, {"full", true, 65536, true}, {"small", true, 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{
				a: &a64.Asm{}, m: &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{HasMax: tc.hasMax, Max: tc.max}}}},
				locals:          []localDef{{reg: X19}, {reg: X20}, {reg: X21}},
				pinnedLocalMask: regMask(0).add(X19).add(X20).add(X21),
				linearSumLoop:   1 | 3<<16,
			}
			if !f.tryUnrolledLinearSumLatch(1) {
				t.Fatal("unrolled latch not selected")
			}
			var dispatch a64.Asm
			dispatch.MovReg32(X16, X20)
			dispatch.LslImm(X16, X16, 3, false)
			dispatch.Add64(X16, X16, X19)
			dispatch.LsrImm(X16, X16, 32, false)
			site := bytes.Index(f.a.B, dispatch.B)
			if !tc.wrap {
				if site >= 0 {
					t.Fatal("bounded memory emitted an unnecessary wrapping dispatch")
				}
				return
			}
			if site < 0 {
				t.Fatal("wrapping reduction can enter native-offset groups without a scalar dispatch")
			}
			branch := site + len(dispatch.B)
			word := binary.LittleEndian.Uint32(f.a.B[branch:])
			if word&0xff00001f != 0xb5000010 { // cbnz x16, imm19
				t.Fatal("nonzero widened high bits do not select the scalar tail")
			}
			target := branch + int(int32(word<<8)>>13)*4
			var tail a64.Asm
			tail.Cbz32(X20)
			tail.LoadIdx(X16, linMemReg, X19, 0, 8, false, true)
			tail.Add64(X21, X21, X16)
			tail.AddImm32(X19, X19, 8)
			tail.SubsImm32(X20, X20, 1)
			tail.Bcond(condNE)
			if target < 0 || target+len(tail.B) > len(f.a.B) {
				t.Fatal("scalar dispatch target is outside the latch")
			}
			got := append([]byte(nil), f.a.B[target:target+len(tail.B)]...)
			for _, at := range []int{0, len(got) - 4} {
				binary.LittleEndian.PutUint32(got[at:], binary.LittleEndian.Uint32(got[at:]) & ^uint32(0x7ffff<<5))
			}
			if !bytes.Equal(got, tail.B) {
				t.Fatal("wrapping dispatch does not reach a per-load i32 address increment")
			}
		})
	}
}
