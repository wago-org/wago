//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestTupleCountedGroupCoalescesOnlyUnreadCounter(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, read := range []bool{false, true} {
			var condition, body, tail x64.Asm
			condition.TestSelf(R12, wide)
			source := R9
			if read {
				source = R12
			}
			if wide {
				body.MovReg64(R10, source)
			} else {
				body.MovRegReg32(R10, source)
			}
			body.AluRR(0x01, R10, R9, wide)
			tail.B = append(tail.B, body.B...)
			tail.AluRI(5, R12, 1, wide)
			var a x64.Asm
			f := fn{a: &a, pinnedLocalMask: maskOf(R9, R10, R12)}
			if !f.tryScalarTupleCountedLoop(condition.B, tail.B, condNE, 0) {
				t.Fatal("fixture failed counted-loop admission")
			}
			end := a.Len() - 5
			group := end + int(int32(binary.LittleEndian.Uint32(a.B[end-4:end])))
			var compare, want x64.Asm
			compare.AluRI(7, R12, 4, wide)
			for i := 0; i < 4; i++ {
				if read {
					want.B = append(want.B, tail.B...)
				} else {
					want.B = append(want.B, body.B...)
				}
			}
			if !read {
				want.AluRI(5, R12, 4, wide)
			}
			got := a.B[group : end-6-len(compare.B)]
			if !bytes.Equal(got, want.B) {
				t.Fatalf("wide=%t counterRead=%t group=%x want=%x", wide, read, got, want.B)
			}
		}
	}
}
