//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func cachedWriteFunction(t *testing.T) *fn {
	t.Helper()
	f := &fn{a: &enc.Asm{}, s: newStack(), sc: &scratch{}, globalCellReg: regNone, traceFuncIdx: 7, wasmPC: 19}
	f.m = modMem(t, 1, nil, nil, []byte{0, 0x0b})
	f.guardMode = true
	t.Cleanup(f.checkEndLifetimes)
	return f
}

func openStoreForwardCheck(t *testing.T, f *fn, wide bool) Reg {
	t.Helper()
	size, typ, load := 4, mtI32, byte(0x28)
	if wide {
		size, typ, load = 8, mtI64, 0x29
	}
	f.storeForwardOK = true
	f.pinnedLocalMask = maskOf(R13)
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R13})
	f.pushReg(R12, typ)
	r := wasm.ReaderFrom([]byte{0, 0, 0x20, 0, load, 0, 0})
	if err := f.memStore(&r, size); err != nil {
		t.Fatal(err)
	}
	if !f.storeFwd.valid {
		t.Fatal("store-forward window did not open")
	}
	return f.storeFwd.reg
}

func emitReadInputFP(f *fn, into bool) {
	if into {
		f.fbinInto(3, 0x58, true)
	} else {
		f.fbin(0x58, true)
	}
}

func TestCachedWriteLifetimes(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		f := cachedWriteFunction(t)
		reg := f.globalCellPtr(0)
		f.invalidateGlobalsCache()
		f.a.MovImm64(reg, 0)
		f.globalCellPtr(1)
		f.invalidateGlobalsCache()
		f.a.MovImm64(reg, 0)
	})
	for _, wide := range []bool{false, true} {
		for _, consume := range []bool{false, true} {
			f := cachedWriteFunction(t)
			reg := openStoreForwardCheck(t, f, wide)
			if consume {
				f.pushValue(storage{kind: stLocalRef, typ: mtI32})
				size := 4
				if wide {
					size = 8
				}
				if !f.forwardStoredLoad(0, size, false, wide) {
					t.Fatal("load was not forwarded")
				}
			} else {
				f.invalidateStoreForward()
			}
			f.a.MovImm64(reg, 0)
		}
	}
}

func TestReadInputConsumption(t *testing.T) {
	for _, into := range []bool{false, true} {
		f := cachedWriteFunction(t)
		f.pushFReg(3, mtF64)
		f.pushValue(storage{kind: stConst, typ: mtF64, cval: 0x3ff8000000000000})
		emitReadInputFP(f, into) // the final operation may overwrite its first input
	}
	f := cachedWriteFunction(t)
	f.pinnedLocalMask = maskOf(R13)
	// Address normalization preserves the low 32 bits of this borrowed value.
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R13})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R13})
	r := wasm.ReaderFrom([]byte{0, 0})
	if err := f.memStore(&r, 4); err != nil {
		t.Fatal(err)
	}
}
