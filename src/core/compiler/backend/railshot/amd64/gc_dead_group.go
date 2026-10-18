//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type gcDeadDefaultGroupSite struct {
	typeIndex uint32
	pc        uint32
	layout    nativeGCStructAllocPlan
}

// The first prototype admits one group at function entry, with no local or
// operand state. It neither moves nor removes a fallback allocation safepoint.
func (f *fn) tryDeadGCDefaultGroup(r *wasm.Reader, firstType uint32) (bool, error) {
	if !f.opt(optDeadGCNew) || !f.opt(optGCNativeAlloc) || f.unreachable ||
		profileEnabled || f.inlineDepth != 0 || f.moduleEH || len(f.customInstructions) != 0 ||
		len(f.ctrl) != 1 || len(f.localType) != 0 || f.depth() != 0 || f.nGlobalPins != 0 ||
		f.wasmPC != f.tracePCBase {
		return false, nil
	}
	var sites [4]gcDeadDefaultGroupSite
	look := *r
	n := 0
	typeIndex, pc := firstType, f.wasmPC
	for {
		layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
		if !ok || len(layout.Type.Comp.Fields) > 16 || !gcStructHasOnlyDirectScalars(*layout.Type) {
			break
		}
		plan, ok := f.nativeGCStructAllocLayout(typeIndex)
		if !ok || !plan.pointerFree || plan.objectSize == 0 {
			break
		}
		next := look
		drop, err := next.Byte()
		if err != nil || drop != 0x1a {
			break
		}
		sites[n] = gcDeadDefaultGroupSite{typeIndex: typeIndex, pc: pc, layout: plan}
		n++
		if n == len(sites) {
			break
		}
		pc = f.tracePCBase + uint32(next.Offset())
		op, err := next.Byte()
		if err != nil || op != 0xfb {
			break
		}
		sub, err := next.U32()
		if err != nil || sub != 1 {
			break
		}
		typeIndex, err = next.U32()
		if err != nil {
			break
		}
		// Keep look at the end of the last complete admitted pair.
		probe := next
		found, err := probe.Byte()
		if err != nil || found != 0x1a {
			break
		}
		// The next iteration consumes that drop after checking the bounded layout.
		look = next
	}
	if n < 2 {
		return false, nil
	}
	// Re-read the accepted bounded group to obtain its exact continuation. The
	// loop above can leave look before the rejected next pair's drop.
	continuation := *r
	for i := range n {
		if i != 0 {
			_, _ = continuation.Byte()
			_, _ = continuation.U32()
			_, _ = continuation.U32()
		}
		_, _ = continuation.Byte()
	}
	f.flush()
	f.invalidateStoreForward()
	f.invalidateGCMutableLoadFacts()
	f.emitDeadGCDefaultGroupCharge(sites[:n])
	nativeDone := f.a.JccPlaceholder(condNE)
	originalPC := f.wasmPC
	for _, site := range sites[:n] {
		f.wasmPC = site.pc
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(site.typeIndex)})
		if err := f.callGCStructHelper(gcStructReserveDeadNative, []wasm.ValType{wasm.I32}, nil); err != nil {
			return true, err
		}
	}
	f.wasmPC = originalPC
	f.a.PatchRel32(nativeDone, f.a.Len())
	*r = continuation
	f.stats.peep("gc-dead-default-group")
	return true, nil
}

// Like the existing dead scalar charge, a live reservation batch proves native
// allocation remains enabled. No state changes until the complete group fits.
func (f *fn) emitDeadGCDefaultGroupCharge(sites []gcDeadDefaultGroupSite) {
	a := f.a
	for _, reg := range []Reg{R8, R9, R10, R11} {
		a.Push(reg)
	}
	var fallback []int
	fail := func(cond Cond) { fallback = append(fallback, a.JccPlaceholder(cond)) }
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	var largest uint32
	for _, site := range sites {
		if site.layout.objectSize > largest {
			largest = site.layout.objectSize
		}
	}
	a.CmpImm32Mem(R8, gc.NativeViewNurseryObjectMaxBytesOffset, int32(largest))
	fail(condBE)
	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.TestSelf(R10, true)
	fail(condE)
	a.Load32(RAX, R10, gc.NativeStructAllocCountOffset)
	a.TestSelf(RAX, false)
	fail(condE)
	a.AluRI(cmpDigit, RAX, gc.NativeStructAllocHandleCapacity, false)
	fail(condA)
	a.Load64(R9, R8, gc.NativeViewNurseryBumpOffset)
	a.TestSelf(R9, true)
	fail(condE)
	a.Load32(RCX, R9, 0)
	// 64-bit arithmetic prevents wrap before the final uint32 capacity proof.
	for _, site := range sites {
		align := int32(site.layout.objectAlign)
		a.AluRI(0, RCX, align-1, true)
		a.AluRI(4, RCX, -align, true)
		a.AluRI(0, RCX, int32(site.layout.objectSize), true)
	}
	a.Load32(RDX, R8, gc.NativeViewNurseryAllocBytesOffset)
	a.Cmp64(RCX, RDX)
	fail(condA)
	a.Load64(R10, R8, gc.NativeViewAllocationCountOffset)
	a.TestSelf(R10, true)
	fail(condE)
	a.Store32(R9, 0, RCX)
	a.Load64(RAX, R10, 0)
	a.AluRI(0, RAX, int32(len(sites)), true)
	a.Store64(R10, 0, RAX)
	a.Load64(RAX, R10, gc.NativeAllocationMinorCollectionsOffset)
	a.Load64(RDX, R10, gc.NativeAllocationFullCollectionsOffset)
	a.Add64(RAX, RDX)
	a.Load64(RDX, R8, gc.NativeViewDeadNurseryCycleOffset)
	a.Cmp64(RAX, RDX)
	current := a.JccPlaceholder(condE)
	a.XorSelf32(RDX)
	a.Store32(R10, gc.NativeAllocationDeadObjectsOffset, RDX)
	a.Store64(R8, gc.NativeViewDeadNurseryCycleOffset, RAX)
	a.PatchRel32(current, a.Len())
	a.Load32(RAX, R10, gc.NativeAllocationDeadObjectsOffset)
	a.AluRI(0, RAX, int32(len(sites)), false)
	a.Store32(R10, gc.NativeAllocationDeadObjectsOffset, RAX)
	a.MovImm32(RAX, 1)
	done := a.JmpPlaceholder()
	for _, patch := range fallback {
		a.PatchRel32(patch, a.Len())
	}
	a.XorSelf32(RAX)
	a.PatchRel32(done, a.Len())
	a.TestSelf(RAX, false)
	for _, reg := range []Reg{R11, R10, R9, R8} {
		a.Pop(reg)
	}
}
