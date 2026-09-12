//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// FixedHostCallView is a preselected zero-copy portal over the root control
// frame for one scalar host import with a fixed signature.
type FixedHostCallView func(args, results []uint64)

func (e *Engine) CallWithHostBaseFixedView(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, host HostCall, fixed FixedHostCallView) error {
	if linMemBase == 0 {
		return fmt.Errorf("jit: host-call linear-memory base is zero")
	}
	if fixed == nil {
		return fmt.Errorf("jit: fixed host-call view is nil")
	}
	n, nres := int(rawSlots&0xffff), int(rawSlots>>16)
	if n > maxHostArity || nres > maxHostArity {
		return fmt.Errorf("jit: fixed host-call view has %d parameter slots and %d result slots", n, nres)
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if err := InitHostCtrlFrame(ctrl); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	rootArgs := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcArgs])), n)
	rootResults := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcResults])), nres)
	callErr := e.callWithHostLoopFixedView(code, serArgs, linMemBase, trap, results, ctrl, slicePtr(ctrl), host, fixed, rootArgs, rootResults)
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return callErr
}

func (e *Engine) callWithHostLoopFixedView(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, rootCtrl []byte, rootCtrlPtr uintptr, host HostCall, fixed FixedHostCallView, rootArgs, rootResults []uint64) error {
	ctrl, ctrlPtr := rootCtrl, rootCtrlPtr
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
				clear(rootResults)
				fixed(rootArgs, rootResults)
				continue
			}

			ctrl = hostCtrlFrame(ctrlPtr)
			imp := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
			raw := binary.LittleEndian.Uint32(ctrl[hcNArgs:])
			n, nres := int(raw&0xffff), int(raw>>16)
			if n > maxHostArity || nres > maxHostArity {
				argsArea, resultsArea, capacity, err := hostCtrlWideCallAreas(ctrl, n, nres)
				if err != nil {
					return err
				}
				args := unsafe.Slice((*uint64)(unsafe.Pointer(&argsArea[0])), capacity)[:n]
				wideResults := unsafe.Slice((*uint64)(unsafe.Pointer(&resultsArea[0])), capacity)[:nres]
				clear(wideResults)
				host(ctrlPtr, imp, args, wideResults)
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
