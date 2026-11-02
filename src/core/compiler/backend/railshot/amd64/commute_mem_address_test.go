//go:build amd64

package amd64

import (
	"bytes"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

// Compile only: a merge destination can be the owned address of the left
// deferred load. Commuting that load to the RHS must not overwrite its address.
func TestCommuteMemoryPreservesDestinationAddress(t *testing.T) {
	saved := commuteMemLeftEnabled
	commuteMemLeftEnabled = true
	t.Cleanup(func() { commuteMemLeftEnabled = saved })
	for _, wide := range []bool{false, true} {
		f, left := deferredAddressFunction(t, wide, -1)
		typ := mtI32
		if wide {
			typ = mtI64
		}
		right := f.pushReg(RDI, typ)
		node := f.s.alloc()
		node.setElemKind(ekDeferred)
		node.setDeferredOp(opXor)
		node.setValueType(typ)
		node.arg0, node.arg1 = left, right
		f.s.pushDeferred(node)
		f.condenseBinary(node, R12)
		// Independently verify actual emission in every build, not checker metadata:
		// read through the original address before accumulating the register input.
		var want enc.Asm
		want.LoadIdx(R12, RBX, R12, 8, left.st.memSize(), false, wide)
		want.AluRR(aluTable[opXor].rr, R12, RDI, wide)
		if !bytes.Equal(f.a.B, want.B) {
			t.Fatalf("wide=%v code=%x want=%x", wide, f.a.B, want.B)
		}
	}
}
