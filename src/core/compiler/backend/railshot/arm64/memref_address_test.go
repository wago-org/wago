//go:build arm64

package arm64

import (
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/arm64"
)

func deferredAddressFunction(t *testing.T, wide bool, borrow int) (*fn, *elem) {
	t.Helper()
	f := &fn{a: &enc.Asm{}, s: newStack(), sc: &scratch{}, traceFuncIdx: 7, wasmPC: 19}
	size := 4
	if wide {
		size = 8
	}
	e := f.pushValue(memRefStorage(X12, 8, size, false, wide, borrow, borrow))
	if borrow < 0 {
		f.regUser[X12] = e
	} else {
		f.pinnedLocalMask = maskOf(X12)
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
						f.condenseInto(e, X0)
					case "alu", "multiply":
						dst := X0
						if borrow < 0 {
							dst = X12 // read and overwrite the address in one instruction
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
						f.loadMemRef(X0, e)
					}
					// A consumed address can be reused even while its old node remains.
					f.a.MovImm64(X12, 0)
				})
			}
		}
	}
}
