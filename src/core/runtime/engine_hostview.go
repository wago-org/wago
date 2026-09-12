//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// HostCallView is an optional zero-copy portal over the parked native control
// frame. args and results are borrowed for the duration of the call; returning
// handled=false falls back to HostCall, including for cross-instance frames.
type HostCallView func(ctrl uintptr, importIdx uint32, args, results []uint64) (handled bool)

// CallWithHostBaseView exposes the parked argument and result areas directly to
// an eligible host callback. It lives outside engine.go so adding this uncommon
// wide-signature loop cannot perturb the generic and typed scalar loop layout.
func (e *Engine) CallWithHostBaseView(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, host HostCall, view HostCallView) error {
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
	callErr := e.callWithHostLoopView(code, serArgs, linMemBase, trap, results, ctrl, slicePtr(ctrl), host, view)
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return callErr
}

func (e *Engine) callWithHostLoopView(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, ctrlPtr uintptr, host HostCall, view HostCallView) error {
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
				args := unsafe.Slice((*uint64)(unsafe.Pointer(&argsArea[0])), capacity)[:n]
				wideResults := unsafe.Slice((*uint64)(unsafe.Pointer(&resultsArea[0])), capacity)[:nres]
				clear(wideResults)
				if view(ctrlPtr, imp, args, wideResults) {
					continue
				}
				host(ctrlPtr, imp, args, wideResults)
				continue
			}
			args := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcArgs])), n)
			directResults := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcResults])), nres)
			clear(directResults)
			if view(ctrlPtr, imp, args, directResults) {
				continue
			}
			e.callHostCopied(ctrl, ctrlPtr, imp, n, nres, host)
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
	}
}

// callHostCopied preserves the generic inline-buffer boundary when the direct
// portal declines a foreign control frame. This is cold for the single-import
// view, so keeping it out of callWithHostLoopView also keeps the wide hot loop
// compact.
func (e *Engine) callHostCopied(ctrl []byte, ctrlPtr uintptr, imp uint32, n, nres int, host HostCall) {
	if e.hostScratchInUse {
		var args, results [maxHostArity]uint64
		callHostCopiedWith(ctrl, ctrlPtr, imp, n, nres, host, args[:], results[:])
		return
	}
	e.hostScratchInUse = true
	defer func() { e.hostScratchInUse = false }()
	callHostCopiedWith(ctrl, ctrlPtr, imp, n, nres, host, e.hostArgs[:], e.hostResults[:])
}

func callHostCopiedWith(ctrl []byte, ctrlPtr uintptr, imp uint32, n, nres int, host HostCall, args, results []uint64) {
	for i := 0; i < n; i++ {
		args[i] = binary.LittleEndian.Uint64(ctrl[hcArgs+i*8:])
	}
	clear(results[:nres])
	host(ctrlPtr, imp, args[:n], results[:nres])
	for i := 0; i < nres; i++ {
		binary.LittleEndian.PutUint64(ctrl[hcResults+i*8:], results[i])
	}
}
