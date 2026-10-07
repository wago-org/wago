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

const HostCtrlImportIndexOffset = hcImportIdx

// CallWithHostBaseMultiViewBounded dispatches an immutable set of admitted
// numeric Go imports, checking each selected import before borrowing its slots.
func (e *Engine) CallWithHostBaseMultiViewBounded(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, signatures []uint32, fixed FixedHostCallView) error {
	if !access.Granted() || linMemBase == 0 || fixed == nil || len(signatures) < 2 || len(signatures) > 64 {
		return fmt.Errorf("jit: invalid bounded multi-host admission")
	}
	for _, shape := range signatures {
		if shape&0xffff > maxHostArity || shape>>16 > maxHostArity {
			return fmt.Errorf("jit: invalid bounded multi-host shape")
		}
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if err := initBoundedGoHostCtrlFrame(ctrl); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	defer func() {
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(serArgs)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
		goruntime.KeepAlive(signatures)
	}()
	if used, err := e.tryInlineBoundedMultiHostView(code, serArgs, linMemBase, trap, results, ctrl, signatures, fixed); used {
		return err
	}
	args := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcArgs])), maxHostArity)
	values := unsafe.Slice((*uint64)(unsafe.Pointer(&ctrl[hcResults])), maxHostArity)
	root := slicePtr(ctrl)
	enterNativeBounded(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	for {
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
			index := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
			if uintptr(binary.LittleEndian.Uint64(trap[8:])) != root || uint64(index) >= uint64(len(signatures)) || binary.LittleEndian.Uint32(ctrl[hcNArgs:]) != signatures[index] {
				return fmt.Errorf("jit: bounded multi-host segment escaped admitted import")
			}
			shape := signatures[index]
			n, nres := int(shape&0xffff), int(shape>>16)
			clear(values[:nres])
			fixed(args[:n:n], values[:nres:nres])
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
