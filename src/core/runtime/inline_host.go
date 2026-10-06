//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The bounded scalar bridge retains Wago's fixed dispatcher (including its
// native lease), while replacing each park/return/resume with a live Go frame.
// Only the existing module-wide bounded Go-segment admission reaches here.
//
//go:noescape
func inlineHostEnter(fn FixedScalarHostCall, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64

func inlineHostBridgeAddr() uintptr

const inlineHostTrapCellOffset = abi.TrapCellPtrOffset

var inlineHostEnabled = scalarGoABI(goruntime.Version()) && os.Getenv("WAGO_"+strings.ToUpper(goruntime.GOARCH)+"_NO_INLINE_HOST") != "1"

func (e *Engine) tryInlineBoundedHost(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedScalarHostCall) (bool, error) {
	if !inlineHostEnabled {
		return false, nil
	}
	old := binary.LittleEndian.Uint64(ctrl[hcTrampoline:])
	bridge := uint64(inlineHostBridgeAddr())
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], bridge)
	defer func() {
		binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], old)
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(args)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
	}()
	if outcome := inlineHostEnter(fixed, code, slicePtr(args), linMem, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), slots); outcome == 1 {
		return true, fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return true, trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return true, nil
}

//go:noescape
func inlineHostViewEnter(fn FixedHostCallView, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64

func inlineHostViewBridgeAddr() uintptr

func (e *Engine) tryInlineBoundedHostView(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedHostCallView) (bool, error) {
	if !inlineHostEnabled {
		return false, nil
	}
	old := binary.LittleEndian.Uint64(ctrl[hcTrampoline:])
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(inlineHostViewBridgeAddr()))
	defer func() {
		binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], old)
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(args)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
	}()
	if outcome := inlineHostViewEnter(fixed, code, slicePtr(args), linMem, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), slots); outcome == 1 {
		return true, fmt.Errorf("jit: inline host view escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return true, trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return true, nil
}
