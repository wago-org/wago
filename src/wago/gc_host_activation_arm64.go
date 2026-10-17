//go:build (linux || darwin) && arm64

package wago

import (
	"encoding/binary"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type gcHostSavedControl [abi.ARM64SyncHostCallSavedBytes / 8]uint64

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
	control := unsafe.Slice((*byte)(offHeapPtr(ctrl)), abi.ARM64SyncHostCallSavedBytes)
	savedSP := uintptr(binary.LittleEndian.Uint64(control[abi.SyncHostCallSavedNativeSPOffset:]))
	retPC := uintptr(binary.LittleEndian.Uint64(control[abi.ARM64SyncHostCallSavedLROffset:]))
	if savedSP == 0 {
		panic(gcHelperFailuref("generic GC arm64 host activation has invalid saved SP %#x", savedSP))
	}
	findCallsite := func(pc uintptr) int {
		if pc < in.base || pc-in.base >= uintptr(len(in.c.code)) {
			return -1
		}
		rel := uint32(pc - in.base)
		for i := range plan.callsites {
			if plan.callsites[i].returnOffset == rel {
				return i
			}
		}
		return -1
	}
	thunkAdjust := uintptr(0)
	callsite := findCallsite(retPC)
	if callsite < 0 {
		// Dynamic/owned sync thunks reserve 32 bytes and preserve their incoming
		// module LR at SP+16. Their host-stub LR may itself be in the code blob but
		// is not a logical Wasm callsite, so fall through to the saved module LR.
		if savedSP > ^uintptr(0)-16 {
			panic(gcHelperFailuref("generic GC arm64 host thunk frame overflows"))
		}
		retPC = uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(savedSP+16)), 8)))
		thunkAdjust = 32
		callsite = findCallsite(retPC)
	}
	if callsite < 0 && !gcBridge {
		panic(gcHelperFailuref("generic GC arm64 host activation return PC %#x has no callsite map", retPC))
	}
	var base uintptr
	if callsite >= 0 {
		adjust := thunkAdjust + uintptr(plan.callsites[callsite].stackAdjust)
		if savedSP > ^uintptr(0)-adjust {
			panic(gcHelperFailuref("generic GC arm64 host activation frame base overflows"))
		}
		base = savedSP + adjust
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
	control := unsafe.Slice((*byte)(offHeapPtr(activation.ctrl)), abi.ARM64SyncHostCallSavedBytes)
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
