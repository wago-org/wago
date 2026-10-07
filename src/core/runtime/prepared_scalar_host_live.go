//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"os"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

var preparedScalarHostEnabled = os.Getenv("WAGO_NO_PREPARED_SCALAR_HOST") != "1"

func preparedScalarHostAvailable() bool     { return inlineHostEnabled && preparedScalarHostEnabled }
func preparedScalarHostBridge() uintptr     { return inlineHostBridgeAddr() }
func preparedScalarHostViewBridge() uintptr { return inlineHostViewBridgeAddr() }

// Call keeps dynamic native ownership outside the bridge. Memory base and
// engine stack top remain live; every transfer retains the existing assembly
// shape/root/trap checks and a normal rooted Go callback frame.
func (p *PreparedScalarHost) Call(code, linMem uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	if p == nil || p.engine == nil || p.mode != 0 || linMem == 0 || context == nil || fixed == nil {
		return fmt.Errorf("jit: invalid prepared scalar host call")
	}
	if p.detached != nil {
		linMem = p.detachedBase()
	}
	clearTrapUnlessInterrupted(p.trap)
	storeOffHeapU64(linMem-abi.TrapCellPtrOffset, uint64(slicePtr(p.trap)))
	binary.LittleEndian.PutUint64(p.ctrl[hcTrampoline:], uint64(p.bridge))
	defer func() {
		binary.LittleEndian.PutUint64(p.ctrl[hcTrampoline:], uint64(p.staged))
		goruntime.KeepAlive(p)
		goruntime.KeepAlive(context)
	}()
	if outcome := inlineHostContextEnter(fixed, context, code, slicePtr(p.args), linMem, slicePtr(p.trap), slicePtr(p.results), p.engine.stackTop, slicePtr(p.ctrl), p.slots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(p.trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), p.trap)
	}
	return nil
}

// CallView retains result clearing and borrowed-view shape checks in the live owner.
func (p *PreparedScalarHost) CallView(code, linMem uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	if p == nil || p.engine == nil || (goruntime.GOARCH == "amd64" && p.mode != 1 || goruntime.GOARCH != "amd64" && p.mode == 0) || linMem == 0 || context == nil || fixed == nil {
		return fmt.Errorf("jit: invalid prepared scalar host call")
	}
	if p.detached != nil {
		linMem = p.detachedBase()
	}
	clearTrapUnlessInterrupted(p.trap)
	storeOffHeapU64(linMem-abi.TrapCellPtrOffset, uint64(slicePtr(p.trap)))
	binary.LittleEndian.PutUint64(p.ctrl[hcTrampoline:], uint64(p.bridge))
	defer func() {
		binary.LittleEndian.PutUint64(p.ctrl[hcTrampoline:], uint64(p.staged))
		goruntime.KeepAlive(p)
		goruntime.KeepAlive(context)
	}()
	if outcome := inlineHostViewContextEnter(fixed, context, code, slicePtr(p.args), linMem, slicePtr(p.trap), slicePtr(p.results), p.engine.stackTop, slicePtr(p.ctrl), p.slots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(p.trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), p.trap)
	}
	return nil
}
