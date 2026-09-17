//go:build linux && amd64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

type gcHostSavedControl [8]uint64

type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func (in *Instance) pushGCHostActivation(ctrl uintptr, dispatch uint32, stackTop uintptr) gcHostActivationToken {
	if in == nil || ctrl == 0 || dispatch&gcStructDispatchBit != 0 {
		return gcHostActivationToken{}
	}
	if in.c == nil || in.c.genericGCFrameRoots() == nil {
		return gcHostActivationToken{}
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
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation has invalid saved RSP %#x", savedRSP)})
	}
	returnSlot := savedRSP
	retPC := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(returnSlot)), 8)))
	plan, codeBase, codeBytes, callsite := in.gcCompilerCallsite(retPC)
	if callsite < 0 {
		// Dynamic/ref-call host paths can add one or more bounded wrapper records
		// above the host stub. Scan only the fixed wrapper envelope and accept a
		// candidate solely when it equals a validated logical callsite return PC.
		const wrapperScanBytes = uintptr(512)
		if stackTop <= savedRSP+8 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation saved RSP %#x is outside the foreign stack", savedRSP)})
		}
		limit := wrapperScanBytes
		if available := stackTop - savedRSP - 8; available < limit {
			limit = available
		}
		limit &^= 7
		for off := uintptr(8); off <= limit; off += 8 {
			candidateSlot := savedRSP + off
			candidatePC := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(candidateSlot)), 8)))
			if candidatePlan, candidateBase, candidateBytes, candidate := in.gcCompilerCallsite(candidatePC); candidate >= 0 {
				returnSlot, retPC, plan, codeBase, codeBytes, callsite = candidateSlot, candidatePC, candidatePlan, candidateBase, candidateBytes, candidate
				break
			}
		}
	}
	if callsite < 0 && !gcBridge {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation return PC %#x has no callsite map in the bounded wrapper envelope", retPC)})
	}
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.hostActivationCount >= gcHostActivationLimit {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation depth exceeds %d", gcHostActivationLimit)})
	}
	index := state.hostActivationCount
	activation := &state.hostActivations[index]
	activation.ctrl = ctrl
	if callsite < 0 {
		// A proper tail to a Runtime-owned host thunk has already discarded the
		// Wasm caller frame. Its compact arguments are rooted separately before the
		// native lease is released, so there is no frame chain to walk.
		activation.noFrame = true
	} else {
		if returnSlot > ^uintptr(0)-abi.AMD64CallReturnAddressBytes-uintptr(plan.callsites[callsite].stackAdjust) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation frame base overflows")})
		}
		activation.base = returnSlot + abi.AMD64CallReturnAddressBytes + uintptr(plan.callsites[callsite].stackAdjust)
		activation.callsite = uint32(callsite)
		activation.rootPlan = plan
		activation.codeBase = codeBase
		activation.codeBytes = codeBytes
	}
	for i := range activation.savedControl {
		activation.savedControl[i] = binary.LittleEndian.Uint64(control[i*8:])
	}
	state.hostActivationCount++
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
}
