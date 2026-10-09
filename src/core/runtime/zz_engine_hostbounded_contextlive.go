//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// CallWithHostBaseScalarBoundedContextLive calls a static target with a rooted
// Go context, avoiding a method-value wrapper on each import. The separate
// scalar fallback preserves disabled-bridge behavior without capturing context
// in a closure, which would force the activation onto the heap.
func (e *Engine) CallWithHostBaseScalarBoundedContextLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, context unsafe.Pointer, fixed FixedScalarHostContextCall, fallback FixedScalarHostCall) error {
	if !inlineHostEnabled {
		return e.CallWithHostBaseScalarBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fallback)
	}
	if !access.Granted() || linMemBase == 0 || fixed == nil || context == nil || rawSlots>>16 > 2 || rawSlots&0xffff > 2 {
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
	old := binary.LittleEndian.Uint64(ctrl[hcTrampoline:])
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(inlineHostBridgeAddr()))
	defer func() {
		binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], old)
		goruntime.KeepAlive(serArgs)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(context)
	}()
	if outcome := inlineHostContextEnter(fixed, context, code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), rawSlots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return nil
}

// Three Go ABI arguments use 24 bytes of outgoing spill space; the native
// landing metadata remains at 32/40 bytes in the 64-byte owner frame. The
// context pointer stays in the owner argument map across callback stack growth.
//
//go:noescape
func inlineHostContextEnter(fn FixedScalarHostContextCall, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64
