//go:build arm64

package arm64

import (
	"fmt"
	"reflect"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func gpClassEmitter() *fn {
	return &fn{a: &a64.Asm{}, s: newStackWithCap(minStackArenaCap), sc: &scratch{}}
}

func allAllocatableGP() regMask {
	var mask regMask
	for _, r := range gpAlloc {
		mask = mask.add(r)
	}
	return mask
}

// X0 and V0 have the same numeric index but independent owners. A GP request
// must skip the older vector-bank value and spill the actual X0 owner.
func TestGPSpillSkipsFloatAndVectorOwners(t *testing.T) {
	for _, typ := range []machineType{mtF32, mtF64, mtV128} {
		t.Run(fmt.Sprintf("type=%d", typ), func(t *testing.T) {
			f := gpClassEmitter()
			f.reserved = allAllocatableGP().remove(X0)
			fp := f.pushFReg(0, typ)
			before := fp.st
			gp := f.pushReg(X0, mtI64)
			if got := f.allocRegOrNone(0); got != X0 {
				t.Fatalf("allocated register %d, want X0", got)
			}
			if !reflect.DeepEqual(fp.st, before) || f.fregUser[0] != fp {
				t.Fatal("GP allocation changed the V0 value or its owner")
			}
			if gp.st.kind != stSlot || gp.st.typ != mtI64 || f.regUser[X0] != nil {
				t.Fatal("GP allocation did not spill the X0 value")
			}
			if f.maxSpill != 1 {
				t.Fatalf("spill slots=%d, want one GP slot", f.maxSpill)
			}
		})
	}
}

func TestGPSpillAllRegistersBlockedPreservesBothBanks(t *testing.T) {
	for _, source := range []string{"avoid", "pinned", "local", "reserved"} {
		t.Run(source, func(t *testing.T) {
			f := gpClassEmitter()
			fp := f.pushFReg(0, mtV128)
			gp := f.pushReg(X0, mtI64)
			fpBefore, gpBefore := fp.st, gp.st
			var avoid regMask
			switch source {
			case "avoid":
				avoid = allAllocatableGP()
			case "pinned":
				f.pinned = allAllocatableGP()
			case "local":
				f.pinnedLocalMask = allAllocatableGP()
			case "reserved":
				f.reserved = allAllocatableGP()
			}
			if got := f.allocRegOrNone(avoid); got != regNone {
				t.Fatalf("allocated blocked register %d", got)
			}
			if !reflect.DeepEqual(fp.st, fpBefore) || !reflect.DeepEqual(gp.st, gpBefore) ||
				f.fregUser[0] != fp || f.regUser[X0] != gp || len(f.a.B) != 0 || f.maxSpill != 0 {
				t.Fatal("failed GP allocation changed register owners, metadata or code")
			}
		})
	}
}

// A deferred float load is different from a resident FP value: its storage
// owns a GP address. The allocator must still materialize and spill that load
// to release the address, while it preserves an older V0 value.
func TestGPSpillCanReleaseDeferredFloatAddress(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(fmt.Sprintf("f64=%t", wide), func(t *testing.T) {
			f := gpClassEmitter()
			f.reserved = allAllocatableGP().remove(X0)
			fp := f.pushFReg(0, mtV128)
			before := fp.st
			load := f.pushValue(fmemRefStorage(X0, 0, wide, -1, -1))
			f.regUser[X0] = load
			if got := f.allocRegOrNone(0); got != X0 {
				t.Fatalf("allocated register %d, want released address X0", got)
			}
			if !reflect.DeepEqual(fp.st, before) || f.fregUser[0] != fp {
				t.Fatal("address allocation changed the older V0 value")
			}
			if load.st.kind != stSlot || load.st.typ != mtOf2(wide) || f.regUser[X0] != nil {
				t.Fatal("deferred float was not materialized and spilled")
			}
			for r, owner := range f.fregUser {
				if r != 0 && owner != nil {
					t.Fatalf("temporary FP register %d still has an owner", r)
				}
			}
		})
	}
}
