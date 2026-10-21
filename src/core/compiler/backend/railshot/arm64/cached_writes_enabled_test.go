//go:build arm64 && wago_regalloccheck

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestRegallocCachedWrites(t *testing.T) {
	for _, cache := range []string{"global", "store-i32", "store-i64"} {
		for _, write := range []string{"mov32", "mov64", "conditional", "call"} {
			t.Run(cache+"/"+write, func(t *testing.T) {
				f := cachedWriteFunction(t)
				reg, want := regNone, "store-forward value"
				if cache == "global" {
					reg, want = f.globalCellPtr(0), "global-cell pointer"
				} else {
					reg = openStoreForwardCheck(t, f, cache == "store-i64")
				}
				requireAllocationFailure(t, want, func() {
					switch write {
					case "mov32":
						f.a.MovImm32(reg, 0)
					case "conditional":
						f.a.Cset32(reg, condE)
					case "call":
						f.a.Blr(X0)
					default:
						f.a.MovImm64(reg, 0)
					}
				})
			})
		}
	}
}

func TestRegallocCachedWritesTerminalScope(t *testing.T) {
	f := cachedWriteFunction(t)
	cell := f.globalCellPtr(0)
	value := openStoreForwardCheck(t, f, false)
	func() {
		saved := f.checkTerminalWrites()
		defer f.checkRestoreWrites(saved)
		f.a.MovImm64(cell, 0)
		f.a.MovImm64(value, 0)
	}()
	requireAllocationFailure(t, "global-cell pointer", func() { f.a.MovImm64(cell, 0) })
	requireAllocationFailure(t, "store-forward value", func() { f.a.MovImm64(value, 0) })
	f.checkEndLifetimes()
	f.a.MovImm64(cell, 0)
	f.a.MovImm64(value, 0)
}

func TestRegallocReadInputGP(t *testing.T) {
	f := cachedWriteFunction(t)
	// The address constant uses a different register while X12 holds the input.
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 16})
	f.pushReg(X12, mtI32)
	inject := true
	f.a.ObserveGPWrites(func(mask uint32) {
		if inject && mask&(uint32(1)<<X12) == 0 {
			inject = false
			f.a.MovImm64(X12, 0)
		}
	})
	r := wasm.ReaderFrom([]byte{0, 0})
	requireAllocationFailure(t, "read-only GP input", func() {
		if err := f.memStore(&r, 4); err != nil {
			t.Fatal(err)
		}
	})
	if inject {
		t.Fatal("address preparation was not reached")
	}
}

func TestRegallocReadInputGP64Normalization(t *testing.T) {
	f := cachedWriteFunction(t)
	f.pinnedLocalMask = maskOf(X13)
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X13})
	f.pushValue(storage{kind: stLocalReg, typ: mtI64, reg: X13, idx: 1})
	r := wasm.ReaderFrom([]byte{0, 0})
	requireAllocationFailure(t, "read-only GP input", func() {
		if err := f.memStore(&r, 8); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRegallocReadInputFP(t *testing.T) {
	for _, into := range []bool{false, true} {
		for _, borrow := range []bool{false, true} {
			f := cachedWriteFunction(t)
			if borrow {
				f.fpinnedLocalMask = maskOf(3)
				f.pushValue(storage{kind: stLocalReg, typ: mtF64, reg: 3})
			} else {
				f.pushFReg(3, mtF64)
			}
			f.pushValue(storage{kind: stConst, typ: mtF64, cval: 0x3ff8000000000000})
			inject := true
			f.a.ObserveFPWrites(func(mask uint32) {
				if inject && mask&(uint32(1)<<3) == 0 {
					inject = false
					f.a.Fadd(3, 3, 4, true)
				}
			})
			requireAllocationFailure(t, "read-only FP input", func() { emitReadInputFP(f, into) })
			if inject {
				t.Fatal("second operand preparation was not reached")
			}
		}
	}
}
