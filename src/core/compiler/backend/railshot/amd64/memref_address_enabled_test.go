//go:build amd64 && wago_regalloccheck

package amd64

import "testing"

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
							f.a.MovImm32(R12, 0)
						case "partial":
							f.a.SetccReg(condE, R12)
						case "call":
							f.a.CallReg(RAX)
						default:
							f.a.MovImm64(R12, 0)
						}
					})
				})
			}
		}
	}
}

func TestRegallocDeferredLoadAddressSharedBorrow(t *testing.T) {
	f, first := deferredAddressFunction(t, false, 0)
	second := f.pushValue(memRefStorage(R12, 12, 4, false, false, 0))
	second = f.popValue()
	first = f.popValue()
	f.loadMemRef(RAX, first)
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(R12, 0) })
	f.loadMemRef(RAX, second)
	f.a.MovImm64(R12, 0)
}

func TestRegallocDeferredLoadAddressNormalization(t *testing.T) {
	f, _ := deferredAddressFunction(t, false, 0)
	f.m = modMem(t, 1, nil, nil, []byte{0, 0x0b})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R12})
	f.memAddr(16, 4, true, 0)
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(R12, 0) })
}

func TestRegallocDeferredLoadAddressCopiedBorrow(t *testing.T) {
	f, e := deferredAddressFunction(t, false, 0)
	for r := range f.intervalOwner {
		f.intervalOwner[r] = -1
	}
	f.intervalOwner[R12] = 0
	if f.detachControlIntervalAddresses() == 0 {
		t.Fatal("address was not copied")
	}
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(e.st.reg, 0) })
	f.loadMemRef(RAX, e)
	f.a.MovImm64(R12, 0)
}

func TestRegallocDeferredLoadAddressKeepsSiblingLive(t *testing.T) {
	f, first := deferredAddressFunction(t, true, -1)
	f.pushValue(memRefStorage(R13, 0, 8, false, true, -1))
	f.loadMemRef(R12, first) // consuming R12 must not exempt an unrelated address
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(R13, 0) })
}

func TestRegallocDeferredLoadAddressOverwriteKeepsSharedBorrowLive(t *testing.T) {
	f, first := deferredAddressFunction(t, false, -1)
	f.pushValue(memRefStorage(R12, 12, 4, false, false, 0))
	requireAllocationFailure(t, "deferred load address", func() { f.loadMemRef(R12, first) })
}

func TestRegallocDeferredLoadAddressObserverScopes(t *testing.T) {
	f, _ := deferredAddressFunction(t, false, -1)
	func() {
		saved := f.checkTerminalWrites()
		defer f.checkRestoreWrites(saved)
		f.a.MovImm64(R12, 0)
	}()
	requireAllocationFailure(t, "deferred load address", func() { f.a.MovImm64(R12, 0) })
	f.checkEndLifetimes()
	f.checkEndLifetimes()
	f.a.MovImm64(R12, 0)
}

// This is the instruction ordering from PR #972 without its destination guard:
// the left sink overwrites the still-pending right load's address.
func TestRegallocDeferredLoadAddressHintedSink(t *testing.T) {
	for _, wide := range []bool{false, true} {
		f, right := deferredAddressFunction(t, wide, -1)
		left := f.pushValue(storage{kind: stConst, typ: right.st.typ, cval: 20})
		requireAllocationFailure(t, "deferred load address", func() {
			f.condenseInto(left, R12)
			f.applyALU(aluTable[opAdd], R12, right, wide)
		})
	}
}
