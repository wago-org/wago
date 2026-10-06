//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// CallWithHostBaseScalarBoundedLive keeps direct live entry separate from the
// general scalar driver's fallback frame, with the same admission contract.
// Validation and live trampoline ownership share one Go frame.
func (e *Engine) CallWithHostBaseScalarBoundedLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedScalarHostCall) error {
	if !inlineHostEnabled {
		return e.CallWithHostBaseScalarBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
	}
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
	old := binary.LittleEndian.Uint64(ctrl[hcTrampoline:])
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(inlineHostBridgeAddr()))
	defer func() {
		binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], old)
		goruntime.KeepAlive(serArgs)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
		goruntime.KeepAlive(e)
	}()
	if outcome := inlineHostEnter(fixed, code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), rawSlots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return nil
}
