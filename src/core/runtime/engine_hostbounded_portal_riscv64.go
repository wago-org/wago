//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/abi"
	goruntime "runtime"
	"unsafe"
)

type rv64BoundedPortal struct {
	slots         uint32
	signatures    []uint32
	scalar        FixedScalarHostCall
	i32           func(int32) int32
	view          FixedHostCallView
	contextScalar FixedScalarHostContextCall
	contextView   FixedHostContextCallView
}

func (e *Engine) callRV64BoundedHost(code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, p rv64BoundedPortal, context unsafe.Pointer, initialize bool) error {
	if e == nil || e.stackTop == 0 || len(e.stack) == 0 || home == 0 || code == 0 || p.scalar == nil && p.i32 == nil && p.view == nil && p.contextScalar == nil && p.contextView == nil {
		return fmt.Errorf("jit: invalid RV64 bounded host entry")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if len(ctrl) < ctrlFrameSize {
		return fmt.Errorf("jit: host control frame has %d bytes, need %d", len(ctrl), ctrlFrameSize)
	}
	if initialize {
		if err := InitHostCtrlFrame(ctrl); err != nil {
			return err
		}
	}
	stub, err := hostCallStubPtr()
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(stub))
	defer func() {
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(args)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
		goruntime.KeepAlive(p)
		goruntime.KeepAlive(context)
	}()
	clearTrapUnlessInterrupted(trap)
	if TrapCode(loadTrap(trap)) == TrapInterrupted {
		return trapErrorFromBuffer(TrapInterrupted, trap)
	}
	storeOffHeapU64(home-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	root := slicePtr(ctrl)
	e.prepareNativeInvocation(args, results)
	enterNative(code, slicePtr(args), home, slicePtr(trap), slicePtr(results), e.stackTop)
	for {
		if tc := loadTrap(trap); tc != hostCallPending {
			if tc != 0 {
				return trapErrorFromBuffer(TrapCode(tc), trap)
			}
			return nil
		}
		// Authenticate the active frame before reading or lending its slot areas.
		if uintptr(binary.LittleEndian.Uint64(trap[8:])) != root {
			return fmt.Errorf("jit: bounded host segment escaped its admitted control frame")
		}
		index := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
		slots := p.slots
		if p.signatures != nil {
			if uint64(index) >= uint64(len(p.signatures)) {
				return fmt.Errorf("jit: bounded multi-host segment escaped admitted import")
			}
			slots = p.signatures[index]
		} else if index != 0 {
			return fmt.Errorf("jit: bounded host segment escaped its admitted import")
		}
		if binary.LittleEndian.Uint32(ctrl[hcNArgs:]) != slots {
			return fmt.Errorf("jit: bounded host segment escaped its admitted signature")
		}
		n, nres := int(slots&0xffff), int(slots>>16)
		if p.scalar != nil || p.contextScalar != nil || p.i32 != nil {
			var a0, a1, value uint64
			if n != 0 {
				a0 = binary.LittleEndian.Uint64(ctrl[hcArgs:])
			}
			if n == 2 {
				a1 = binary.LittleEndian.Uint64(ctrl[hcArgs+8:])
			}
			if p.i32 != nil {
				value = uint64(uint32(p.i32(int32(a0))))
			} else if p.contextScalar != nil {
				value = p.contextScalar(context, a0, a1)
			} else {
				value = p.scalar(a0, a1)
			}
			if nres == 1 {
				binary.LittleEndian.PutUint64(ctrl[hcResults:], value)
			} else if nres == 2 {
				binary.LittleEndian.PutUint64(ctrl[hcResults:], value&0xffffffff)
				binary.LittleEndian.PutUint64(ctrl[hcResults+8:], value>>32)
			}
		} else {
			in := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcArgs])), n)
			out := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcResults])), nres)
			clear(out)
			if p.contextView != nil {
				p.contextView(context, in, out)
			} else {
				p.view(in, out)
			}
		}
		clearTrapUnlessInterrupted(trap)
		if TrapCode(loadTrap(trap)) == TrapInterrupted {
			return trapErrorFromBuffer(TrapInterrupted, trap)
		}
		prepareHostResume(ctrl, trap, e.stackTop, e.StackLimit())
		resumeNative(root, e.stackTop)
	}
}
