//go:build amd64

package amd64

import (
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func deferredAddressFunction(t *testing.T, wide bool, borrow int) (*fn, *elem) {
	t.Helper()
	f := &fn{a: &enc.Asm{}, s: newStack(), sc: &scratch{}, traceFuncIdx: 7, wasmPC: 19}
	e := f.pushValue(memRefStorage(R12, 8, 4, false, wide, borrow))
	if wide {
		e.st.idx = 8
	}
	if borrow < 0 {
		f.regUser[R12] = e
	} else {
		f.pinnedLocalMask = maskOf(R12)
	}
	t.Cleanup(f.checkEndLifetimes)
	return f, e
}

func TestDeferredLoadAddressConsumption(t *testing.T) {
	for _, wide := range []bool{false, true} {
		name := "i32"
		if wide {
			name = "i64"
		}
		for _, borrow := range []int{-1, 0} {
			owner := "owned"
			if borrow >= 0 {
				owner = "borrowed"
			}
			for _, action := range []string{"materialize", "sink", "alu", "multiply", "spill", "discard", "drop", "popped"} {
				t.Run(name+"/"+owner+"/"+action, func(t *testing.T) {
					f, e := deferredAddressFunction(t, wide, borrow)
					switch action {
					case "materialize":
						f.materialize(e)
					case "sink":
						f.condenseInto(e, RAX)
					case "alu", "multiply":
						dst := RAX
						if borrow < 0 {
							dst = R12 // read and overwrite the address in one instruction
						}
						if action == "alu" {
							f.applyALU(aluTable[opAdd], dst, e, wide)
						} else {
							f.applyMul(dst, e, wide)
						}
					case "spill":
						f.spill(e)
					case "discard":
						f.discardTree(e)
					case "drop":
						f.dropValue()
					case "popped":
						e = f.popValue()
						f.loadMemRef(RAX, e)
					}
					// A consumed address can be reused even while its old node remains.
					f.a.MovImm64(R12, 0)
				})
			}
		}
	}
}
