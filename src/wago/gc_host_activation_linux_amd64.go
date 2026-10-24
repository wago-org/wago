//go:build linux && amd64

package wago

import (
	"encoding/binary"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type gcHostSavedControl [8]uint64

type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func (in *Instance) pushGCHostActivation(ctrl uintptr, dispatch uint32, stack *coreruntime.Engine) gcHostActivationToken {
	if in == nil || ctrl == 0 || dispatch&gcStructDispatchBit != 0 {
		return gcHostActivationToken{}
	}
	plan := in.c.genericGCFrameRoots()
	if plan == nil {
		return gcHostActivationToken{}
	}
	stackTop, stackBytes := stack.StackTop(), uintptr(stack.StackBytes())
	if stackBytes == 0 || stackTop < stackBytes {
		panic(gcHelperFailuref("generic GC host activation stack is unavailable"))
	}
	_, gcBridge := in.pluginGCHostSignature(dispatch)
	if dispatch&hostFuncRefDispatchBit != 0 {
		binding, ok := in.boundHostFuncRef(dispatch)
		if !ok || !binding.owner.isGCBridge() {
			return gcHostActivationToken{}
		}
		gcBridge = true
	}
	control := unsafe.Slice((*byte)(offHeapPtr(ctrl)), 64)
	savedRSP := uintptr(binary.LittleEndian.Uint64(control[abi.SyncHostCallSavedNativeSPOffset:]))
	if savedRSP == 0 || savedRSP > ^uintptr(0)-abi.AMD64CallReturnAddressBytes {
		panic(gcHelperFailuref("generic GC host activation has invalid saved RSP %#x", savedRSP))
	}
	findCallsite := func(pc uintptr) int {
		if pc < in.base || pc-in.base >= uintptr(len(in.c.code)) {
			return -1
		}
		return plan.callsiteIndexByReturnOffset(uint32(pc - in.base))
	}
	returnSlot := savedRSP
	retPC := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(returnSlot)), 8)))
	callsite := findCallsite(retPC)
	if callsite < 0 {
		// Dynamic/ref-call host paths can add one or more bounded wrapper records
		// above the host stub. Scan only the fixed wrapper envelope and accept a
		// candidate solely when it equals a validated logical callsite return PC.
		const wrapperScanBytes = uintptr(512)
		if stackTop <= savedRSP+8 {
			panic(gcHelperFailuref("generic GC host activation saved RSP %#x is outside the foreign stack", savedRSP))
		}
		limit := wrapperScanBytes
		if available := stackTop - savedRSP - 8; available < limit {
			limit = available
		}
		limit &^= 7
		for off := uintptr(8); off <= limit; off += 8 {
			candidateSlot := savedRSP + off
			candidatePC := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(candidateSlot)), 8)))
			if candidate := findCallsite(candidatePC); candidate >= 0 {
				returnSlot, retPC, callsite = candidateSlot, candidatePC, candidate
				break
			}
		}
	}
	if callsite < 0 && !gcBridge {
		panic(gcHelperFailuref("generic GC host activation return PC %#x has no callsite map in the bounded wrapper envelope", retPC))
	}
	var base uintptr
	if callsite >= 0 {
		if returnSlot > ^uintptr(0)-abi.AMD64CallReturnAddressBytes-uintptr(plan.callsites[callsite].stackAdjust) {
			panic(gcHelperFailuref("generic GC host activation frame base overflows"))
		}
		base = returnSlot + abi.AMD64CallReturnAddressBytes + uintptr(plan.callsites[callsite].stackAdjust)
	}
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	index := uint8(0)
	for index < state.hostActivationCount && state.hostActivations[index].stack != nil {
		index++
	}
	if index >= gcHostActivationLimit {
		panic(gcHelperFailuref("generic GC host activation depth exceeds %d", gcHostActivationLimit))
	}
	if index == state.hostActivationCount {
		state.hostActivationCount++
	}
	activation := &state.hostActivations[index]
	activation.ctrl = ctrl
	activation.stack = stack
	activation.noFrame = callsite < 0
	activation.base = base
	if callsite >= 0 {
		activation.callsite = uint32(callsite)
	}
	for i := range activation.savedControl {
		activation.savedControl[i] = binary.LittleEndian.Uint64(control[i*8:])
	}
	state.hostRootPlan = plan
	state.hostCodeBase = in.base
	state.hostCodeBytes = uintptr(len(in.c.code))
	return gcHostActivationToken{state: state, index: index}
}

func (in *Instance) popGCHostActivation(token gcHostActivationToken) {
	state := token.state
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if token.index >= state.hostActivationCount || state.hostActivations[token.index].stack == nil {
		panic(gcHelperFailuref("generic GC host activation token is inactive"))
	}
	activation := &state.hostActivations[token.index]
	control := unsafe.Slice((*byte)(offHeapPtr(activation.ctrl)), 64)
	for i, value := range activation.savedControl {
		binary.LittleEndian.PutUint64(control[i*8:], value)
	}
	*activation = gcHostActivation{}
	// Concurrent foreign roots need not resume in suspension order. Preserve
	// occupied slots and trim only the empty suffix; new activations reuse holes.
	for state.hostActivationCount > 0 && state.hostActivations[state.hostActivationCount-1].stack == nil {
		state.hostActivationCount--
	}
	if state.hostActivationCount == 0 {
		state.hostRootPlan = nil
		state.hostCodeBase = 0
		state.hostCodeBytes = 0
	}
}
