//go:build amd64

package amd64

import (
	"os"

	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// This trial is off by default. Only immutable check results survive calls;
// the address is always loaded again from the current collector view.
var gcCheckedRangeEnabled = os.Getenv("WAGO_AMD64_GC_CHECKED_RANGE") == "1"

func (f *fn) gcCheckedRangeReceiver(local int, typeIndex uint32) bool {
	if !gcCheckedRangeEnabled || !gcResolveReuseEnabled || !f.gcLocalTypeFactsEnabled() ||
		f.inlineDepth != 0 || f.localBase != 0 || local != 0 || len(f.ft.Params) == 0 {
		return false
	}
	fact, ok := gcReferenceFact(f.ft.Params[0])
	return ok && fact.flags&gcReferenceFactNullable == 0 && fact.typeIndex == typeIndex
}

// Only the full final-cast field resolver may publish this proof. It checked
// both handle size and object-header size. A type-only cast is insufficient.
func (f *fn) recordGCCheckedRange(local int, typeIndex, required uint32) {
	if required > 65535 || !f.gcCheckedRangeReceiver(local, typeIndex) {
		return
	}
	fact, _ := f.gcLocalTypeFacts.get(local)
	if (fact.valid() || fact.flags&gcReferenceFactCheckedRange != 0) && fact.typeIndex != typeIndex {
		return
	}
	fact.typeIndex = typeIndex
	fact.flags |= gcReferenceFactCheckedRange
	fact.checkedBytes = max(fact.checkedBytes, uint16(required))
	f.gcLocalTypeFacts.set(local, fact)
}

func (f *fn) reloadGCCheckedRange(local int, typeIndex, required uint32) (Reg, bool) {
	if !f.gcCheckedRangeReceiver(local, typeIndex) {
		return regNone, false
	}
	fact, _ := f.gcLocalTypeFacts.get(local)
	if fact.flags&gcReferenceFactCheckedRange == 0 || fact.typeIndex != typeIndex || uint32(fact.checkedBytes) < required {
		return regNone, false
	}
	// Restrict the trial to an update through an older copy of this receiver.
	// Keeping that copy in place lets the existing raw-address cache serve the
	// later setter. No address survives a call or a collection point.
	object := f.s.back()
	olderLocal, ok := gcLocalProvenance(object.prev)
	if !ok || olderLocal != local {
		return regNone, false
	}
	obj := f.gcResolvedRegister()
	if obj == regNone {
		return regNone, false
	}
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved).add(obj)
	var scratch [2]Reg
	n := 0
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !block.has(r) {
			scratch[n] = r
			block = block.add(r)
			n++
			if n == len(scratch) {
				break
			}
		}
	}
	if n != len(scratch) {
		return regNone, false
	}
	view, entry := scratch[0], scratch[1]
	f.pinned = f.pinned.add(obj).add(view).add(entry)
	ref, owned := f.materializeRead(f.popValue())
	f.a.MovRegReg32(entry, ref)
	if owned {
		f.release(ref)
	}
	f.a.Load64(view, RBX, -int32(abi.GCNativeViewPtrOffset))
	f.a.Load64(view, view, gc.NativeInstanceViewCollectorOffset)
	f.a.Load64(obj, view, gc.NativeViewHandlesOffset)
	// Ref = handle << 1; handle entries are 16 bytes.
	f.a.LeaScaled(entry, obj, entry, 3, 0)
	f.a.Load32(obj, entry, gc.NativeHandleOffsetOffset)
	f.a.LoadIdx(entry, entry, RSP, gc.NativeHandleSpaceOffset, 1, false, false)
	f.a.ShiftImm(4, entry, 4, false)
	f.a.LoadIdx(view, view, entry, gc.NativeViewSpacesOffset+gc.NativeSpaceBaseOffset, 8, false, true)
	f.a.Add64(obj, view)
	f.pinned = f.pinned.remove(view).remove(entry)
	f.gcResolved = gcResolvedObject{valid: true, local: local, typeIndex: typeIndex, requiredBytes: required, reg: obj}
	f.stats.peep("gc-checked-range-reload")
	return obj, true
}
