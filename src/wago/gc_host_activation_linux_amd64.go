//go:build linux && amd64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func (in *Instance) pushGCHostActivation(ctrl uintptr, dispatch uint32) gcHostActivationToken {
	if in == nil || ctrl == 0 || dispatch&gcStructDispatchBit != 0 || dispatch&hostFuncRefDispatchBit != 0 {
		return gcHostActivationToken{}
	}
	plan := in.c.genericGCFrameRoots()
	if plan == nil {
		return gcHostActivationToken{}
	}
	control := unsafe.Slice((*byte)(offHeapPtr(ctrl)), 64)
	savedRSP := uintptr(binary.LittleEndian.Uint64(control[abi.SyncHostCallSavedNativeSPOffset:]))
	if savedRSP == 0 || savedRSP > ^uintptr(0)-abi.AMD64CallReturnAddressBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation has invalid saved RSP %#x", savedRSP)})
	}
	returnSlot := savedRSP
	retPC := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(returnSlot)), 8)))
	if retPC < in.base || retPC-in.base >= uintptr(len(in.c.Code)) {
		// Dynamic host-import thunks save RBX and the wrapper result pointer before
		// parking. The module call's return address is therefore three qwords above
		// the stub return address. Validate that address against this module before
		// treating it as an activation boundary.
		if savedRSP > ^uintptr(0)-24 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation wrapper stack overflows")})
		}
		returnSlot = savedRSP + 24
		retPC = uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(returnSlot)), 8)))
		if retPC < in.base || retPC-in.base >= uintptr(len(in.c.Code)) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation return PC %#x is outside module code", retPC)})
		}
	}
	rel := uint32(retPC - in.base)
	callsite := -1
	for i := range plan.callsites {
		if plan.callsites[i].returnOffset == rel {
			callsite = i
			break
		}
	}
	if callsite < 0 {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation return offset %d has no callsite map", rel)})
	}
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.hostActivationCount >= gcHostActivationLimit {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation depth exceeds %d", gcHostActivationLimit)})
	}
	index := state.hostActivationCount
	activation := &state.hostActivations[index]
	if returnSlot > ^uintptr(0)-abi.AMD64CallReturnAddressBytes-uintptr(plan.callsites[callsite].stackAdjust) {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation frame base overflows")})
	}
	activation.base = returnSlot + abi.AMD64CallReturnAddressBytes + uintptr(plan.callsites[callsite].stackAdjust)
	activation.ctrl = ctrl
	activation.callsite = uint32(callsite)
	for i := range activation.savedControl {
		activation.savedControl[i] = binary.LittleEndian.Uint64(control[i*8:])
	}
	state.hostActivationCount++
	state.hostRootPlan = plan
	state.hostCodeBase = in.base
	state.hostCodeBytes = uintptr(len(in.c.Code))
	return gcHostActivationToken{state: state, index: index}
}

func (in *Instance) popGCHostActivation(token gcHostActivationToken) {
	state := token.state
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.hostActivationCount == 0 || token.index != state.hostActivationCount-1 {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation stack is not LIFO")})
	}
	activation := &state.hostActivations[token.index]
	control := unsafe.Slice((*byte)(offHeapPtr(activation.ctrl)), 64)
	for i, value := range activation.savedControl {
		binary.LittleEndian.PutUint64(control[i*8:], value)
	}
	*activation = gcHostActivation{}
	state.hostActivationCount--
	if state.hostActivationCount == 0 {
		state.hostRootPlan = nil
		state.hostCodeBase = 0
		state.hostCodeBytes = 0
	}
}
