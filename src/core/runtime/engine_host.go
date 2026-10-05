//go:build (linux || darwin || windows) && (amd64 || arm64) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Architecture-owned parked-host state stays out of unsupported targets.
type engineHostState struct {
	hostScratchInUse bool
	hostArgs         [maxHostArity]uint64
	hostResults      [maxHostArity]uint64
}

// CallWithHost runs native code that may request returning host imports via the
// synchronous re-entry protocol. The first crossing is a
// normal enterNative; whenever native code parks at a host call (trap cell ==
// hostCallPending), the bound host function is run here — on the goroutine stack,
// in normal Go context, so arbitrary host code is safe (no foreign-stack /
// morestack hazard) — and native code is resumed via resumeNative.
//
// ctrl must point at an off-heap control frame of at least ctrlFrameSize bytes
// whose address has been installed as the import ctx via JobMemory.SetCustomCtx.
// A cross-instance callee may park through a different frame; its stub publishes
// that exact pointer at trap+8 so dispatch and resume follow the active callee.
func (e *Engine) CallWithHost(code uintptr, serArgs, linMem, trap, results, ctrl []byte, host HostCall) error {
	err := e.CallWithHostBase(code, serArgs, slicePtr(linMem), trap, results, ctrl, host)
	goruntime.KeepAlive(linMem)
	return err
}

// CallWithHostBase is the stable-base form used by guard-page JobMemory, whose
// reserved linear-memory base may not be representable by LinearMemory's Go
// slice. The guard handler is installed/registered by JobMemory creation.
func (e *Engine) CallWithHostBase(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, host HostCall) error {
	return e.callWithHostBase(code, serArgs, linMemBase, trap, results, ctrl, host, nil)
}

// ScalarHostCall is an optional fixed-slot portal for the common scalar import
// shapes. rawSlots packs parameter slots in the low 16 bits and result slots in
// the high 16 bits. Returning handled=false preserves the generic slice path.
type ScalarHostCall func(ctrl uintptr, importIdx, rawSlots uint32, a0, a1 uint64) (result uint64, handled bool)

// FixedScalarHostCall is the preselected portal for a root instance with one
// capability-free scalar import. The engine validates the fixed slot shape once
// at entry and uses the generic callbacks for any cross-instance control frame.
type FixedScalarHostCall func(a0, a1 uint64) (result uint64)

// CallWithHostBaseScalar adds a fixed-slot portal without changing the generic
// host callback contract used for unsupported signatures and cross-instance
// frames.
func (e *Engine) CallWithHostBaseScalar(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, host HostCall, scalar ScalarHostCall) error {
	return e.callWithHostBase(code, serArgs, linMemBase, trap, results, ctrl, host, scalar)
}

// PreparedHostScalarCall is a validated host-capable native entry. Its buffers
// and JobMemory remain owned by the caller and must outlive the prepared call.
// Keeping them behind this opaque handle prevents the hot path from accepting
// unchecked pointers or short control/trap buffers on every invocation.
type PreparedHostScalarCall struct {
	engine          *Engine
	code            uintptr
	serArgs         []byte
	linMemBase      uintptr
	trap            []byte
	results         []byte
	ctrl            []byte
	fixedSlots      uint32
	fixed           bool
	boundedSegments bool
}

// PrepareHostScalarCall validates and binds a reservation-held scalar host
// entry. The returned handle is not safe for concurrent use.
func (e *Engine) PrepareHostScalarCall(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, memory *JobMemory, trap, results, ctrl []byte) (*PreparedHostScalarCall, error) {
	return e.prepareHostScalarCall(access, code, serArgs, memory, trap, results, ctrl)
}

// PrepareHostScalarFixedCall validates and binds a reservation-held scalar host
// entry whose root import has one immutable, compact slot shape. Foreign control
// frames still use the complete checked dispatcher.
func (e *Engine) PrepareHostScalarFixedCall(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, memory *JobMemory, trap, results, ctrl []byte, rawSlots uint32) (*PreparedHostScalarCall, error) {
	n, nres := int(rawSlots&0xffff), int(rawSlots>>16)
	if n > 2 || nres > 2 {
		return nil, fmt.Errorf("jit: fixed scalar host shape has %d parameter slots and %d result slots", n, nres)
	}
	prepared, err := e.prepareHostScalarCall(access, code, serArgs, memory, trap, results, ctrl)
	if err != nil {
		return nil, err
	}
	prepared.fixedSlots = rawSlots
	prepared.fixed = true
	return prepared, nil
}

func (e *Engine) prepareHostScalarCall(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, memory *JobMemory, trap, results, ctrl []byte) (*PreparedHostScalarCall, error) {
	if !access.Granted() {
		return nil, fmt.Errorf("jit: prepared host-call access denied")
	}
	if e == nil {
		return nil, fmt.Errorf("jit: nil engine")
	}
	if len(e.stack) < int(MinNativeStackBytes) || e.stackTop == 0 {
		return nil, fmt.Errorf("jit: host-call engine is not initialized")
	}
	if code == 0 {
		return nil, fmt.Errorf("jit: host-call code address is zero")
	}
	if memory == nil {
		return nil, fmt.Errorf("jit: host-call memory is nil")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return nil, err
	}
	if err := InitHostCtrlFrame(ctrl); err != nil {
		return nil, err
	}
	if err := memory.RebindTrapCell(trap); err != nil {
		return nil, err
	}
	memory.SetStackFence(e.StackLimit())
	memory.SetCustomCtx(slicePtr(ctrl))
	return &PreparedHostScalarCall{engine: e, code: code, serArgs: serArgs, linMemBase: memory.LinMemBase(), trap: trap, results: results, ctrl: ctrl}, nil
}

// Call enters the validated reservation-held host loop.
func (p *PreparedHostScalarCall) Call(host HostCall, scalar ScalarHostCall) error {
	if p == nil || p.engine == nil {
		return fmt.Errorf("jit: nil prepared host scalar call")
	}
	if host == nil {
		return fmt.Errorf("jit: prepared host call dispatcher is nil")
	}
	if scalar == nil {
		return fmt.Errorf("jit: prepared scalar host portal is nil")
	}
	clearTrapUnlessInterrupted(p.trap)
	ctrlPtr := slicePtr(p.ctrl)
	var callErr error
	if p.engine.hostScratchInUse {
		var argBuf, resBuf [maxHostArity]uint64
		callErr = p.engine.callWithHostLoop(p.code, p.serArgs, p.linMemBase, p.trap, p.results, p.ctrl, ctrlPtr, host, scalar, argBuf[:], resBuf[:])
	} else {
		p.engine.hostScratchInUse = true
		defer func() { p.engine.hostScratchInUse = false }()
		callErr = p.engine.callWithHostLoop(p.code, p.serArgs, p.linMemBase, p.trap, p.results, p.ctrl, ctrlPtr, host, scalar, p.engine.hostArgs[:], p.engine.hostResults[:])
	}
	goruntime.KeepAlive(p)
	return callErr
}

// CallFixed enters a prepared host loop whose root import and scalar slot shape
// were validated when the handle was created. Nested or foreign control frames
// retain the generic host and scalar dispatchers.
func (p *PreparedHostScalarCall) CallFixed(host HostCall, scalar ScalarHostCall, fixed FixedScalarHostCall) error {
	if p == nil || p.engine == nil || !p.fixed {
		return fmt.Errorf("jit: nil or non-fixed prepared host scalar call")
	}
	if host == nil {
		return fmt.Errorf("jit: prepared host call dispatcher is nil")
	}
	if scalar == nil {
		return fmt.Errorf("jit: prepared scalar host portal is nil")
	}
	if fixed == nil {
		return fmt.Errorf("jit: prepared fixed scalar host portal is nil")
	}
	clearTrapUnlessInterrupted(p.trap)
	ctrlPtr := slicePtr(p.ctrl)
	var callErr error
	if p.boundedSegments {
		callErr = p.engine.callWithBoundedHostLoop(p.code, p.serArgs, p.linMemBase, p.trap, p.results, p.ctrl, p.fixedSlots, fixed)
	} else {
		callErr = p.engine.callWithHostLoopFixed(p.code, p.serArgs, p.linMemBase, p.trap, p.results, p.ctrl, ctrlPtr, p.fixedSlots, host, scalar, fixed, nil, nil)
	}
	goruntime.KeepAlive(p)
	return callErr
}

// CallWithHostBaseScalarExpanded enables the fixed-slot portal for zero, one,
// or two results. The separate entry keeps the established one-result loop's
// register allocation and code layout unchanged.
func (e *Engine) CallWithHostBaseScalarExpanded(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, host HostCall, scalar ScalarHostCall) error {
	if linMemBase == 0 {
		return fmt.Errorf("jit: host-call linear-memory base is zero")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if err := InitHostCtrlFrame(ctrl); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	ctrlPtr := slicePtr(ctrl)
	var callErr error
	if e.hostScratchInUse {
		var argBuf, resBuf [maxHostArity]uint64
		callErr = e.callWithHostLoopExpanded(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, host, scalar, argBuf[:], resBuf[:])
	} else {
		e.hostScratchInUse = true
		defer func() { e.hostScratchInUse = false }()
		callErr = e.callWithHostLoopExpanded(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, host, scalar, e.hostArgs[:], e.hostResults[:])
	}
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return callErr
}

func (e *Engine) callWithHostBase(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, host HostCall, scalar ScalarHostCall) error {
	if linMemBase == 0 {
		return fmt.Errorf("jit: host-call linear-memory base is zero")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if err := InitHostCtrlFrame(ctrl); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	ctrlPtr := slicePtr(ctrl)
	var callErr error
	if e.hostScratchInUse {
		var argBuf, resBuf [maxHostArity]uint64
		callErr = e.callWithHostLoop(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, host, scalar, argBuf[:], resBuf[:])
	} else {
		e.hostScratchInUse = true
		defer func() { e.hostScratchInUse = false }()
		callErr = e.callWithHostLoop(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, host, scalar, e.hostArgs[:], e.hostResults[:])
	}
	// Native frames can retain these addresses across every host park/resume.
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return callErr
}

// InitHostCtrlFrame installs the shared host-call trampoline in an off-heap
// control frame. Instantiate calls it eagerly so a callee that has never been a
// public root can still park when reached through a cross-instance call.
func InitHostCtrlFrame(ctrl []byte) error {
	if len(ctrl) < ctrlFrameSize {
		return fmt.Errorf("jit: host control frame has %d bytes, need %d", len(ctrl), ctrlFrameSize)
	}
	stub, err := hostCallStubPtr()
	if err != nil {
		return fmt.Errorf("jit: host-call stub: %w", err)
	}
	if _, err := initHostCtrlExtension(ctrl); err != nil {
		return err
	}
	if leaf := nativeScalarLeafPtr(ctrl); leaf != 0 {
		stub = leaf
	}
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(stub))
	return nil
}

func hostCtrlFrame(ptr uintptr) []byte {
	if n, ok := registeredHostCtrlFrames.Load(ptr); ok {
		return unsafe.Slice((*byte)(offHeapPointer(ptr)), n.(int))
	}
	return unsafe.Slice((*byte)(offHeapPointer(ptr)), ctrlFrameSize)
}

func (e *Engine) callWithHostLoop(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, ctrlPtr uintptr, host HostCall, scalar ScalarHostCall, argBuf, resBuf []uint64) error {
	rootCtrl, rootCtrlPtr := ctrl, ctrlPtr
	for first := true; ; first = false {
		if first {
			enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
		} else {
			clearTrapUnlessInterrupted(trap)
			if TrapCode(loadTrap(trap)) == TrapInterrupted {
				return trapErrorFromBuffer(TrapInterrupted, trap)
			}
			stackTop := e.StackTop()
			prepareHostResume(ctrl, trap, stackTop, e.StackLimit())
			resumeNative(ctrlPtr, stackTop)
		}
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
			ctrlPtr = uintptr(binary.LittleEndian.Uint64(trap[8:]))
			if ctrlPtr == 0 {
				return fmt.Errorf("jit: host call did not publish an active control frame")
			}
			if ctrlPtr == rootCtrlPtr {
				ctrl = rootCtrl
			} else {
				ctrl = hostCtrlFrame(ctrlPtr)
			}
			imp := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
			raw := binary.LittleEndian.Uint32(ctrl[hcNArgs:])
			n := int(raw & 0xffff)
			nres := int(raw >> 16)
			if n > maxHostArity || nres > maxHostArity {
				argsArea, resultsArea, capacity, err := hostCtrlWideCallAreas(ctrl, n, nres)
				if err != nil {
					return err
				}
				args := unsafe.Slice((*uint64)(unsafe.Pointer(&argsArea[0])), capacity)
				wideResults := unsafe.Slice((*uint64)(unsafe.Pointer(&resultsArea[0])), capacity)
				clear(wideResults[:nres])
				host(ctrlPtr, imp, args[:n], wideResults[:nres])
				continue
			}
			if scalar != nil && n <= 2 && nres == 1 {
				var a0, a1 uint64
				if n != 0 {
					a0 = binary.LittleEndian.Uint64(ctrl[hcArgs:])
				}
				if n == 2 {
					a1 = binary.LittleEndian.Uint64(ctrl[hcArgs+8:])
				}
				if result, handled := scalar(ctrlPtr, imp, raw, a0, a1); handled {
					binary.LittleEndian.PutUint64(ctrl[hcResults:], result)
					continue
				}
			}
			for k := 0; k < n; k++ {
				argBuf[k] = binary.LittleEndian.Uint64(ctrl[hcArgs+k*8:])
			}
			for k := 0; k < nres; k++ {
				resBuf[k] = 0
			}
			host(ctrlPtr, imp, argBuf[:n], resBuf[:nres])
			for k := 0; k < nres; k++ {
				binary.LittleEndian.PutUint64(ctrl[hcResults+k*8:], resBuf[k])
			}
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
	}
}

func (e *Engine) callWithHostLoopExpanded(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, ctrlPtr uintptr, host HostCall, scalar ScalarHostCall, argBuf, resBuf []uint64) error {
	rootCtrl, rootCtrlPtr := ctrl, ctrlPtr
	// The host-call re-entry loop is intentionally unbounded: a single guest
	// invocation may legitimately make an arbitrary number of host calls (e.g. a
	// long-running rule that polls Date.now()/Math.random() in a loop). A fixed
	// re-entry cap would turn such a guest into an opaque hard error *before* its
	// deadline, pre-empting the cooperative interrupt. The runaway-guest guard is
	// the trap cell: a cancelled/expired context arms TrapInterrupted, the guest
	// traps at the next function-entry/loop-header safepoint, and that surfaces
	// here as `tc != 0` — breaking the loop with the interrupt code rather than a
	// synthetic "too many host calls" error. A guest with no deadline that spins
	// on host calls forever is no different from one that spins on compute
	// forever: both require the caller to arm a timeout.
	for first := true; ; first = false {
		if first {
			enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
		} else {
			clearTrapUnlessInterrupted(trap) // clear host-pending, but preserve concurrent Close interruption
			if TrapCode(loadTrap(trap)) == TrapInterrupted {
				return trapErrorFromBuffer(TrapInterrupted, trap)
			}
			stackTop := e.StackTop()
			prepareHostResume(ctrl, trap, stackTop, e.StackLimit())
			resumeNative(ctrlPtr, stackTop)
		}
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
			ctrlPtr = uintptr(binary.LittleEndian.Uint64(trap[8:]))
			if ctrlPtr == 0 {
				return fmt.Errorf("jit: host call did not publish an active control frame")
			}
			if ctrlPtr == rootCtrlPtr {
				ctrl = rootCtrl
			} else {
				ctrl = hostCtrlFrame(ctrlPtr)
			}
			imp := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
			// hcNArgs packs the call's slot counts: low 16 bits = param slots
			// (native->Go), high 16 bits = result slots (Go->native). Copying only
			// the real result count — not all maxHostArity slots — drops ~15 wasted
			// slot zeroings + copy-backs on the common 0/1-result host call, the hot
			// part of the wasm->host round trip.
			raw := binary.LittleEndian.Uint32(ctrl[hcNArgs:])
			n := int(raw & 0xffff)
			nres := int(raw >> 16)
			if n > maxHostArity || nres > maxHostArity {
				argsArea, resultsArea, capacity, err := hostCtrlWideCallAreas(ctrl, n, nres)
				if err != nil {
					return err
				}
				args := unsafe.Slice((*uint64)(unsafe.Pointer(&argsArea[0])), capacity)
				wideResults := unsafe.Slice((*uint64)(unsafe.Pointer(&resultsArea[0])), capacity)
				clear(wideResults[:nres])
				host(ctrlPtr, imp, args[:n], wideResults[:nres])
				continue
			}
			if scalar != nil && n <= 2 && nres == 1 {
				var a0, a1 uint64
				if n != 0 {
					a0 = binary.LittleEndian.Uint64(ctrl[hcArgs:])
				}
				if n == 2 {
					a1 = binary.LittleEndian.Uint64(ctrl[hcArgs+8:])
				}
				if result, handled := scalar(ctrlPtr, imp, raw, a0, a1); handled {
					binary.LittleEndian.PutUint64(ctrl[hcResults:], result)
					continue
				}
			}
			if scalar != nil && n <= 2 && (nres == 0 || nres == 2) {
				var a0, a1 uint64
				if n != 0 {
					a0 = binary.LittleEndian.Uint64(ctrl[hcArgs:])
				}
				if n == 2 {
					a1 = binary.LittleEndian.Uint64(ctrl[hcArgs+8:])
				}
				if result, handled := scalar(ctrlPtr, imp, raw, a0, a1); handled {
					if nres == 2 {
						binary.LittleEndian.PutUint64(ctrl[hcResults:], result&0xffffffff)
						binary.LittleEndian.PutUint64(ctrl[hcResults+8:], result>>32)
					}
					continue
				}
			}
			for k := 0; k < n; k++ {
				argBuf[k] = binary.LittleEndian.Uint64(ctrl[hcArgs+k*8:])
			}
			for k := 0; k < nres; k++ {
				resBuf[k] = 0
			}
			host(ctrlPtr, imp, argBuf[:n], resBuf[:nres])
			for k := 0; k < nres; k++ {
				binary.LittleEndian.PutUint64(ctrl[hcResults+k*8:], resBuf[k])
			}
			// loop: resumeNative continues native code after the host call
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
	}
}
