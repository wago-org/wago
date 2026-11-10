//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestLinearSumWrappingGroupsUseScalarTailAMD64(t *testing.T) {
	// Inspect emission only: executing the boundary case needs a 4 GiB memory.
	// After the scalar peel, start=fffffff0/count=5 leaves addr=fffffff8 and
	// count=4. Native group offsets would read fffffff8, 100000000, ...;
	// the scalar tail must instead read fffffff8, 0, 8, 10.
	for _, tc := range []struct {
		name   string
		hasMax bool
		max    uint64
		wrap   bool
	}{{"unbounded", false, 0, true}, {"full", true, 65536, true}, {"small", true, 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{
				a: &x86.Asm{}, m: &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{HasMax: tc.hasMax, Max: tc.max}}}},
				locals:          []localDef{{reg: R12}, {reg: R13}, {reg: R14}},
				pinnedLocalMask: regMask(0).add(R12).add(R13).add(R14),
				linearSumLoop:   1 | 3<<16,
			}
			if !f.tryUnrolledLinearSumLatch(nil, 1) {
				t.Fatal("unrolled latch not selected")
			}
			// RDI is the first free accumulator. Require widened arithmetic and
			// a flag-preserving reset before selecting the existing scalar tail.
			var dispatch x86.Asm
			dispatch.MovRegReg32(RDI, R13)
			dispatch.ShiftImm(4, RDI, 3, true)
			dispatch.Add64(RDI, R12)
			dispatch.ShiftImm(5, RDI, 32, true)
			dispatch.MovImm64(RDI, 0)
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
			if !bytes.HasPrefix(f.a.B[branch:], []byte{0x0f, 0x85}) { // jne rel32
				t.Fatal("nonzero widened high bits do not select the scalar tail")
			}
			target := branch + 6 + int(int32(binary.LittleEndian.Uint32(f.a.B[branch+2:])))
			var tail x86.Asm
			tail.TestSelf(R13, false)
			zero := tail.JccPlaceholder(condE)
			tail.AluIdx(aluTable[opAdd].rm, R14, RBX, R12, 0, true)
			tail.AluRI(aluTable[opAdd].digit, R12, 8, false)
			tail.AluRI(aluTable[opSub].digit, R13, 1, false)
			tail.JccPlaceholder(condNE)
			if target < 0 || target+len(tail.B) > len(f.a.B) {
				t.Fatal("scalar dispatch target is outside the latch")
			}
			got := append([]byte(nil), f.a.B[target:target+len(tail.B)]...)
			clear(got[zero : zero+4])
			clear(got[len(got)-4:])
			if !bytes.Equal(got, tail.B) {
				t.Fatal("wrapping dispatch does not reach a per-load i32 address increment")
			}
		})
	}
}
