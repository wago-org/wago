//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// CallWithHostBaseScalarFixed removes per-callback import and signature
// decoding for the single-import scalar shape selected by the caller. Foreign
// control frames still take the complete checked dispatch path.
func (e *Engine) CallWithHostBaseScalarFixed(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, host HostCall, scalar ScalarHostCall, fixed FixedScalarHostCall) error {
	if linMemBase == 0 {
		return fmt.Errorf("jit: host-call linear-memory base is zero")
	}
	if fixed == nil {
		return fmt.Errorf("jit: fixed scalar host portal is nil")
	}
	n, nres := int(rawSlots&0xffff), int(rawSlots>>16)
	if n > 2 || nres > 2 {
		return fmt.Errorf("jit: fixed scalar host shape has %d parameter slots and %d result slots", n, nres)
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
		callErr = e.callWithHostLoopFixed(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, rawSlots, host, scalar, fixed, argBuf[:], resBuf[:])
	} else {
		e.hostScratchInUse = true
		defer func() { e.hostScratchInUse = false }()
		callErr = e.callWithHostLoopFixed(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, rawSlots, host, scalar, fixed, e.hostArgs[:], e.hostResults[:])
	}
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return callErr
}

func (e *Engine) callWithHostLoopFixed(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, ctrlPtr uintptr, rawSlots uint32, host HostCall, scalar ScalarHostCall, fixed FixedScalarHostCall, argBuf, resBuf []uint64) error {
	rootCtrl, rootCtrlPtr := ctrl, ctrlPtr
	n, nres := int(rawSlots&0xffff), int(rawSlots>>16)
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
				var a0, a1 uint64
				if n != 0 {
					a0 = *(*uint64)(unsafe.Pointer(&ctrl[hcArgs]))
				}
				if n == 2 {
					a1 = *(*uint64)(unsafe.Pointer(&ctrl[hcArgs+8]))
				}
				result := fixed(a0, a1)
				if nres == 1 {
					*(*uint64)(unsafe.Pointer(&ctrl[hcResults])) = result
				} else if nres == 2 {
					*(*uint64)(unsafe.Pointer(&ctrl[hcResults])) = result & 0xffffffff
					*(*uint64)(unsafe.Pointer(&ctrl[hcResults+8])) = result >> 32
				}
				continue
			}

			ctrl = hostCtrlFrame(ctrlPtr)
			imp := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
			raw := binary.LittleEndian.Uint32(ctrl[hcNArgs:])
			foreignN := int(raw & 0xffff)
			foreignNres := int(raw >> 16)
			if foreignN > maxHostArity || foreignNres > maxHostArity {
				argsArea, resultsArea, capacity, err := hostCtrlWideCallAreas(ctrl, foreignN, foreignNres)
				if err != nil {
					return err
				}
				args := unsafe.Slice((*uint64)(unsafe.Pointer(&argsArea[0])), capacity)
				wideResults := unsafe.Slice((*uint64)(unsafe.Pointer(&resultsArea[0])), capacity)
				clear(wideResults[:foreignNres])
				host(ctrlPtr, imp, args[:foreignN], wideResults[:foreignNres])
				continue
			}
			if scalar != nil && foreignN <= 2 && foreignNres <= 2 {
				var a0, a1 uint64
				if foreignN != 0 {
					a0 = binary.LittleEndian.Uint64(ctrl[hcArgs:])
				}
				if foreignN == 2 {
					a1 = binary.LittleEndian.Uint64(ctrl[hcArgs+8:])
				}
				if result, handled := scalar(ctrlPtr, imp, raw, a0, a1); handled {
					if foreignNres == 1 {
						binary.LittleEndian.PutUint64(ctrl[hcResults:], result)
					} else if foreignNres == 2 {
						binary.LittleEndian.PutUint64(ctrl[hcResults:], result&0xffffffff)
						binary.LittleEndian.PutUint64(ctrl[hcResults+8:], result>>32)
					}
					continue
				}
			}
			for k := 0; k < foreignN; k++ {
				argBuf[k] = binary.LittleEndian.Uint64(ctrl[hcArgs+k*8:])
			}
			for k := 0; k < foreignNres; k++ {
				resBuf[k] = 0
			}
			host(ctrlPtr, imp, argBuf[:foreignN], resBuf[:foreignNres])
			for k := 0; k < foreignNres; k++ {
				binary.LittleEndian.PutUint64(ctrl[hcResults+k*8:], resBuf[k])
			}
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
	}
}
