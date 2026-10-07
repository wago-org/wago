//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// EnableBoundedSegments requires the caller's compiler proof that every native
// entry and post-host continuation reaches a real Go callback, return, or trap
// within a bounded amount of work. No native imported function may be bound to
// this handle. The proof remains valid across arbitrary Go callback execution.
func (p *PreparedHostScalarCall) EnableBoundedSegments(access runtimebridge.HostScalarCallAccess) error {
	if !access.Granted() || p == nil || !p.fixed || p.fixedSlots>>16 > 2 || p.fixedSlots&0xffff > 2 {
		return fmt.Errorf("jit: invalid bounded host segment admission")
	}
	p.boundedSegments = true
	return nil
}

func (e *Engine) CallWithHostBaseScalarBounded(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedScalarHostCall) error {
	if !access.Granted() || linMemBase == 0 || fixed == nil || rawSlots>>16 > 2 || rawSlots&0xffff > 2 {
		return fmt.Errorf("jit: invalid bounded host segment admission")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if err := initBoundedGoHostCtrlFrame(ctrl); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	var err error
	if goruntime.GOARCH == "arm64" {
		// ARM64 benefits from keeping live entry out of the fallback-loop frame.
		used := false
		used, err = e.tryInlineBoundedHost(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
		if !used {
			err = e.callWithBoundedHostLoop(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
		}
	} else {
		err = e.callWithBoundedHostLoop(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
	}
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return err
}

// Each native segment is bounded; the entire guest invocation may be an
// unbounded host-yielding loop. Go callbacks execute on the normal Go stack
// between segments, retaining Go's ordinary preemption and GC safe points.
func (e *Engine) callWithBoundedHostLoop(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedScalarHostCall) error {
	if used, err := e.tryInlineBoundedHost(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed); used {
		return err
	}
	root := slicePtr(ctrl)
	n := rawSlots & 0xffff
	nres := rawSlots >> 16
	enterNativeBounded(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	for {
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
			if uintptr(binary.LittleEndian.Uint64(trap[8:])) != root ||
				binary.LittleEndian.Uint32(ctrl[hcImportIdx:]) != 0 ||
				binary.LittleEndian.Uint32(ctrl[hcNArgs:]) != rawSlots {
				return fmt.Errorf("jit: bounded host segment escaped its admitted import")
			}
			var a0, a1 uint64
			if n != 0 {
				a0 = binary.LittleEndian.Uint64(ctrl[hcArgs:])
			}
			if n == 2 {
				a1 = binary.LittleEndian.Uint64(ctrl[hcArgs+8:])
			}
			value := fixed(a0, a1)
			if nres == 1 {
				binary.LittleEndian.PutUint64(ctrl[hcResults:], value)
			} else if nres == 2 {
				binary.LittleEndian.PutUint64(ctrl[hcResults:], value&0xffffffff)
				binary.LittleEndian.PutUint64(ctrl[hcResults+8:], value>>32)
			}
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
		clearTrapUnlessInterrupted(trap)
		if TrapCode(loadTrap(trap)) == TrapInterrupted {
			return trapErrorFromBuffer(TrapInterrupted, trap)
		}
		stackTop := e.StackTop()
		prepareHostResume(ctrl, trap, stackTop, e.StackLimit())
		resumeNativeBounded(root, stackTop)
	}
}

// CallWithHostBaseFixedViewBounded keeps the ordinary callback dispatch and
// borrowed slot ABI for signatures outside the compact scalar portal. The
// caller must prove the same module-wide native segment bound and real Go
// import binding required by the scalar variant.
func (e *Engine) CallWithHostBaseFixedViewBounded(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedHostCallView) error {
	n, nres := int(rawSlots&0xffff), int(rawSlots>>16)
	if !access.Granted() || linMemBase == 0 || fixed == nil || n > maxHostArity || nres > maxHostArity {
		return fmt.Errorf("jit: invalid bounded host view admission")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	var initErr error
	if goruntime.GOARCH == "arm64" {
		initErr = initBoundedGoHostCtrlFrame(ctrl)
	} else {
		initErr = InitHostCtrlFrame(ctrl)
	}
	if initErr != nil {
		return initErr
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	var err error
	used := false
	// ARM64 benefits from entering the live bridge here. AMD64's view
	// callbacks are faster with their existing loop entry boundary.
	if goruntime.GOARCH == "arm64" {
		used, err = e.tryInlineBoundedHostView(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
	}
	if !used {
		args := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcArgs])), n)
		values := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcResults])), nres)
		err = e.callWithBoundedHostLoopView(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed, args, values)
	}
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return err
}

func (e *Engine) callWithBoundedHostLoopView(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedHostCallView, args, values []uint64) error {
	if goruntime.GOARCH != "arm64" {
		if used, err := e.tryInlineBoundedHostView(code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed); used {
			return err
		}
	}

	root := slicePtr(ctrl)
	enterNativeBounded(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	for {
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
			if uintptr(binary.LittleEndian.Uint64(trap[8:])) != root || binary.LittleEndian.Uint32(ctrl[hcImportIdx:]) != 0 || binary.LittleEndian.Uint32(ctrl[hcNArgs:]) != rawSlots {
				return fmt.Errorf("jit: bounded host segment escaped its admitted import")
			}
			clear(values)
			fixed(args, values)
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
		clearTrapUnlessInterrupted(trap)
		if TrapCode(loadTrap(trap)) == TrapInterrupted {
			return trapErrorFromBuffer(TrapInterrupted, trap)
		}
		stackTop := e.StackTop()
		prepareHostResume(ctrl, trap, stackTop, e.StackLimit())
		resumeNativeBounded(root, stackTop)
	}
}

// A bounded host segment's admission requires a real Go callback at every
// import edge. Native leaf selection belongs to the general initializer.
func initBoundedGoHostCtrlFrame(ctrl []byte) error {
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
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(stub))
	return nil
}

// FixedScalarHostContextCall carries a rooted Go context separately from its static target.
type FixedScalarHostContextCall func(unsafe.Pointer, uint64, uint64) uint64

// FixedHostContextCallView carries a rooted context and borrowed slot views.
type FixedHostContextCallView func(unsafe.Pointer, []uint64, []uint64)
