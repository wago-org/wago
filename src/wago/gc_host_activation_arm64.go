//go:build (linux || darwin) && arm64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

type gcHostSavedControl [abi.ARM64SyncHostCallSavedBytes / 8]uint64

type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func (in *Instance) pushGCHostActivation(ctrl uintptr, dispatch uint32, _ uintptr) gcHostActivationToken {
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
	control := unsafe.Slice((*byte)(offHeapPtr(ctrl)), abi.ARM64SyncHostCallSavedBytes)
	savedSP := uintptr(binary.LittleEndian.Uint64(control[abi.SyncHostCallSavedNativeSPOffset:]))
	retPC := uintptr(binary.LittleEndian.Uint64(control[abi.ARM64SyncHostCallSavedLROffset:]))
	if savedSP == 0 {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 host activation has invalid saved SP %#x", savedSP)})
	}
	thunkAdjust := uintptr(0)
	plan, codeBase, codeBytes, callsite := in.gcCompilerCallsite(retPC)
	if callsite < 0 {
		// Dynamic/owned sync thunks reserve 32 bytes and preserve their incoming
		// module LR at SP+16. Their host-stub LR may itself be in the code blob but
		// is not a logical Wasm callsite, so fall through to the saved module LR.
		if savedSP > ^uintptr(0)-16 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 host thunk frame overflows")})
		}
		retPC = uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(savedSP+16)), 8)))
		thunkAdjust = 32
		plan, codeBase, codeBytes, callsite = in.gcCompilerCallsite(retPC)
	}
	if callsite < 0 && !gcBridge {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 host activation return PC %#x has no callsite map", retPC)})
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
		activation.noFrame = true
	} else {
		adjust := thunkAdjust + uintptr(plan.callsites[callsite].stackAdjust)
		if savedSP > ^uintptr(0)-adjust {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 host activation frame base overflows")})
		}
		activation.base = savedSP + adjust
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
	control := unsafe.Slice((*byte)(offHeapPtr(activation.ctrl)), abi.ARM64SyncHostCallSavedBytes)
	for i, value := range activation.savedControl {
		binary.LittleEndian.PutUint64(control[i*8:], value)
	}
	*activation = gcHostActivation{}
	state.hostActivationCount--
}
