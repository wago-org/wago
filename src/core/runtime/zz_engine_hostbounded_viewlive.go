//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// CallWithHostBaseFixedViewBoundedLive keeps direct live entry separate from the
// general scalar driver's fallback frame, with the same admission contract.
// Validation and live trampoline ownership share one Go frame.
func (e *Engine) CallWithHostBaseFixedViewBoundedLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedHostCallView) error {
	if !inlineHostEnabled {
		return e.CallWithHostBaseFixedViewBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
	}
	if !access.Granted() || linMemBase == 0 || fixed == nil || rawSlots>>16 > maxHostArity || rawSlots&0xffff > maxHostArity {
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
	}()
	if outcome := inlineHostViewEnter(fixed, code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), rawSlots); outcome == 1 {
		return fmt.Errorf("jit: inline host view escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return nil
}
