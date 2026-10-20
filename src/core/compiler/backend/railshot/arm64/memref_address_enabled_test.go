//go:build arm64 && wago_regalloccheck

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRegallocDeferredLoadAddressRejectsWrites(t *testing.T) {
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
			for _, action := range []string{"mov32", "mov64", "partial", "call", "tree", "popped"} {
				t.Run(name+"/"+owner+"/"+action, func(t *testing.T) {
					f, _ := deferredAddressFunction(t, wide, borrow)
					if action == "tree" {
						f.pushValue(storage{kind: stConst, typ: mtI32, cval: 3})
						f.pushBinOp(opSub, mtI32)
					} else if action == "popped" {
						f.popValue() // detached storage is still live
					}
					requireAllocationFailure(t, "function 7 pc 19: deferred load address", func() {
						switch action {
						case "mov32":
							f.a.MovImm32(X12, 0)
						case "partial":
							f.a.Cset32(X12, condE)
						case "call":
							f.a.Blr(X0)
						default:
							f.a.MovImm64(X12, 0)
						}
					})
				})
			}
		}
	}
}

func TestRegallocDeferredLoadAddressSharedBorrow(t *testing.T) {
	f, first := deferredAddressFunction(t, false, 0)
	second := f.pushValue(memRefStorage(X12, 12, 4, false, false, 0, 0))
	second = f.popValue()
	first = f.popValue()
	f.loadMemRef(X0, first)
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(X12, 0) })
	f.loadMemRef(X0, second)
	f.a.MovImm64(X12, 0)
}

func TestRegallocDeferredLoadAddressNormalization(t *testing.T) {
	f, _ := deferredAddressFunction(t, false, 0)
	f.m = modMem(t, 1, nil, nil, []byte{0, 0x0b})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X12})
	f.memAddr(16, 4, true, 0, false)
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(X12, 0) })
}

func TestRegallocDeferredLoadAddressPair(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, accepted := range []bool{true, false} {
			f, first := deferredAddressFunction(t, wide, -1)
			f.m = modMem(t, 1, nil, nil, []byte{0, 0x0b})
			f.policy = currentCodegenPolicy()
			if !f.opt(optLoadPair) {
				t.Fatal("load pair is disabled")
			}
			first.st.idx |= 1 << 10 // address copied from local 0
			first.st.slot = 0
			if !accepted {
				first.st.slot = uint32(64 * first.st.memSize())
			}
			f.pinnedLocalMask = maskOf(X13)
			f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X13})
			off := first.st.slot + uint32(first.st.memSize())
			r := wasm.ReaderFrom([]byte{0, byte(off&0x7f) | 0x80, byte(off >> 7)})
			if err := f.memLoad(&r, first.st.memSize(), false, wide); err != nil {
				t.Fatal(err)
			}
			if accepted {
				if first.st.kind != stReg {
					t.Fatal("pair was not emitted")
				}
				f.a.MovImm64(X12, 0)
			} else {
				if first.st.kind != stMemRef {
					t.Fatal("rejected pair consumed the first load")
				}
				f.loadMemRef(X0, f.s.back())
				requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(X12, 0) })
			}
		}
	}
}

func TestRegallocDeferredLoadAddressKeepsSiblingLive(t *testing.T) {
	f, first := deferredAddressFunction(t, true, -1)
	f.pushValue(memRefStorage(X13, 0, 8, false, true, -1, -1))
	f.loadMemRef(X12, first) // consuming X12 must not exempt an unrelated address
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(X13, 0) })
}

func TestRegallocDeferredLoadAddressOverwriteKeepsSharedBorrowLive(t *testing.T) {
	f, first := deferredAddressFunction(t, false, -1)
	f.pushValue(memRefStorage(X12, 12, 4, false, false, 0, 0))
	requireAllocationFailure(t, "deferred load address", func() { f.loadMemRef(X12, first) })
}

func TestRegallocDeferredLoadAddressObserverScopes(t *testing.T) {
	f, _ := deferredAddressFunction(t, false, -1)
	func() {
		saved := f.checkTerminalWrites()
		defer f.checkRestoreWrites(saved)
		f.a.MovImm64(X12, 0)
	}()
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(X12, 0) })
	f.checkEndLifetimes()
	f.checkEndLifetimes()
	f.a.MovImm64(X12, 0)
}

// This is the instruction ordering from an unprotected destination hint:
// the left sink overwrites the still-pending right load's address.
func TestRegallocDeferredLoadAddressHintedSink(t *testing.T) {
	for _, wide := range []bool{false, true} {
		f, right := deferredAddressFunction(t, wide, -1)
		left := f.pushValue(storage{kind: stConst, typ: right.st.typ, cval: 20})
		requireAllocationFailure(t, "deferred load address", func() {
			f.condenseInto(left, X12)
			f.applyALU(aluTable[opAdd], X12, right, wide)
		})
	}
}
