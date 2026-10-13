//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type gcHostSavedControl [abi.RV64SyncHostCallSavedBytes / 8]uint64

type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

// pushGCHostActivation publishes the exact rooted Wasm caller while an RV64
// synchronous host thunk has parked native execution. The park prefix records
// the thunk SP; every qualified RV64 wrapper saves its incoming RA in word zero
// of a fixed call-frame header, so the Wasm caller begins immediately after the
// matching record.
func (in *Instance) pushGCHostActivation(ctrl uintptr, dispatch uint32, stack *coreruntime.Engine) gcHostActivationToken {
	if in == nil || ctrl == 0 || dispatch&gcStructDispatchBit != 0 {
		return gcHostActivationToken{}
	}
	plan := in.c.genericGCFrameRoots()
	if plan == nil {
		return gcHostActivationToken{}
	}
	if stack == nil {
		panic(gcHelperFailuref("generic GC RV64 host activation stack is unavailable"))
	}
	stackTop, stackBytes := stack.StackTop(), uintptr(stack.StackBytes())
	if stackBytes == 0 || stackTop < stackBytes {
		panic(gcHelperFailuref("generic GC RV64 host activation stack is unavailable"))
	}
	stackLimit := stackTop - stackBytes
	_, gcBridge := in.pluginGCHostSignature(dispatch)
	if dispatch&hostFuncRefDispatchBit != 0 {
		binding, ok := in.boundHostFuncRef(dispatch)
		if !ok || !binding.owner.isGCBridge() {
			return gcHostActivationToken{}
		}
		gcBridge = true
	}
	control := unsafe.Slice((*byte)(offHeapPtr(ctrl)), abi.RV64SyncHostCallSavedBytes)
	savedSP := uintptr(binary.LittleEndian.Uint64(control[abi.SyncHostCallSavedNativeSPOffset:]))
	savedRA := uintptr(binary.LittleEndian.Uint64(control[8:]))
	if savedSP&15 != 0 || savedSP < stackLimit || savedSP >= stackTop || stackTop-savedSP < 8 {
		panic(gcHelperFailuref("generic GC RV64 host activation has invalid saved SP %#x", savedSP))
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
	// The direct host thunk's saved caller RA is at savedSP. Retained dynamic
	// wrappers may add bounded framed records, so scan only the authenticated
	// foreign stack envelope and accept an exact compiler callsite PC.
	const wrapperScanBytes = uintptr(512)
	returnSlot, callsite := uintptr(0), -1
	limit := wrapperScanBytes
	if available := stackTop - savedSP - 8; available < limit {
		limit = available
	}
	limit &^= 7
	for off := uintptr(0); off <= limit; off += 8 {
		candidateSlot := savedSP + off
		candidatePC := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(candidateSlot)), 8)))
		if candidate := findCallsite(candidatePC); candidate >= 0 {
			returnSlot, callsite = candidateSlot, candidate
			break
		}
	}
	if callsite < 0 && !gcBridge {
		panic(gcHelperFailuref("generic GC RV64 host activation return PC %#x has no callsite map in the bounded wrapper envelope", savedRA))
	}
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.hostActivationCount >= gcHostActivationLimit {
		panic(gcHelperFailuref("generic GC host activation depth exceeds %d", gcHostActivationLimit))
	}
	index := state.hostActivationCount
	activation := &state.hostActivations[index]
	activation.ctrl = ctrl
	activation.stack = stack
	if callsite < 0 {
		activation.noFrame = true
	} else {
		adjust := uintptr(shared.RV64CallFrameHeaderBytes) + uintptr(plan.callsites[callsite].stackAdjust)
		if returnSlot > ^uintptr(0)-adjust {
			panic(gcHelperFailuref("generic GC RV64 host activation frame base overflows"))
		}
		activation.base = returnSlot + adjust
		activation.callsite = uint32(callsite)
		if !gcNativeFrameFitsStack(activation.base, plan.callsites[callsite].frameBytes, stack) {
			panic(gcHelperFailuref("generic GC RV64 host activation frame exceeds stack bounds"))
		}
	}
	for i := range activation.savedControl {
		activation.savedControl[i] = binary.LittleEndian.Uint64(control[i*8:])
	}
	state.hostActivationCount++
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
	if state.hostActivationCount == 0 || token.index != state.hostActivationCount-1 {
		panic(gcHelperFailuref("generic GC host activation stack is not LIFO"))
	}
	activation := &state.hostActivations[token.index]
	control := unsafe.Slice((*byte)(offHeapPtr(activation.ctrl)), abi.RV64SyncHostCallSavedBytes)
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
