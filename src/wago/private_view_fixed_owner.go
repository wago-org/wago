package wago

import (
	goruntime "runtime"
	"unsafe"
)

// This driver keeps the reserved call's guard, native restoration and panic
// recovery in one frame. Revocable admission remains checked on every call.
func (s *PreparedSession) tryPrivateViewFixed2(a0, a1 uint64) (out []uint64, err error, admitted bool) {
	if s == nil || s.state == nil || s.state.closed.Load() {
		return nil, nil, false
	}
	state := s.state
	fn := state.fn
	h := state.privateHost
	if h == nil || h.viewOwner == nil || h.view == nil || fn.paramSlots != 2 || state.fast || state.host || codeProfileEnabled {
		return nil, nil, false
	}
	var gcLease gcInvocationLease
	if err = state.beginCall(&gcLease); err != nil {
		return nil, err, true
	}
	in := fn.in
	if !fn.scalarFast || !fn.boundedNumericHost || fn.gcMaintenance || !in.preparedFastStateValid() || !in.usesIndependentExecution() || in.guestStorageBorrowed() || in.eng.PreparedScalarHost() != h.prepared || offHeapSlicePtr(in.ctrl) != h.activation.ctrl {
		state.endCall(&gcLease)
		return nil, nil, false
	}
	admitted = true
	ownerBegan := false
	defer func() {
		defer state.endCall(&gcLease)
		if ownerBegan {
			h.viewOwner.Restore()
		}
		h.activation.invocation = hostInvocationContext{}
		if r := recover(); r != nil {
			setNativeSyncPanicError(r, &err)
		}
		if err != nil {
			err = in.decorateTrap(err)
		}
	}()
	if activeHostInvocationBindings.Load() != 0 {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	args := nativeUint64Slots(in.serArgs)[:2]
	if !fn.paramWide[0] {
		a0 = uint64(uint32(a0))
	}
	if !fn.paramWide[1] {
		a1 = uint64(uint32(a1))
	}
	args[0], args[1] = a0, a1
	ownerBegan = true
	h.viewOwner.Begin()
	if h.viewOwner.EnterView(fn.entry, unsafe.Pointer(&h.activation), h.view) == 1 {
		err = h.viewOwner.EscapedError()
	} else if code := h.viewOwner.TrapCode(); code != 0 {
		err = h.viewOwner.TrapError(code)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err, true
	}
	out = in.resultVals[:fn.resultSlots]
	if fn.resultSlots == 1 {
		bits := nativeUint64Slots(in.results)[0]
		if !fn.resultWide[0] {
			bits = uint64(uint32(bits))
		}
		out[0] = bits
	} else {
		copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), fn.resultWide, fn.resultWidthClass)
	}
	return out, nil, true
}
