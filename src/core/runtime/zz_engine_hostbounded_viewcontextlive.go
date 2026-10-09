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

// CallWithHostBaseFixedViewBoundedContextLive uses a static callback target and
// a typed Go context root. A separate method-value fallback avoids a capturing
// closure forcing the activation onto the heap when the live bridge is enabled.
func (e *Engine) CallWithHostBaseFixedViewBoundedContextLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, context unsafe.Pointer, fixed FixedHostContextCallView, fallback FixedHostCallView) error {
	if !inlineHostEnabled {
		return e.CallWithHostBaseFixedViewBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fallback)
	}
	if !access.Granted() || linMemBase == 0 || fixed == nil || context == nil || rawSlots>>16 > maxHostArity || rawSlots&0xffff > maxHostArity {
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
	old := binary.LittleEndian.Uint64(ctrl[hcTrampoline:])
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(inlineHostViewBridgeAddr()))
	defer func() {
		binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], old)
		goruntime.KeepAlive(serArgs)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(context)
	}()
	if outcome := inlineHostViewContextEnter(fixed, context, code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), rawSlots); outcome == 1 {
		return fmt.Errorf("jit: inline host view escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return nil
}

// Seven Go ABI argument words use 56 bytes of outgoing spill space; native
// landing metadata stays at 64/72 in the 96-byte owner frame. The typed context
// in the owner argument map is traced and relocated across callback stack growth.
//
//go:noescape
func inlineHostViewContextEnter(fn FixedHostContextCallView, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64
