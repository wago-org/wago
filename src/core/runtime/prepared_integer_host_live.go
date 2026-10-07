//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Integer calls retain a rooted Go owner and reject an ordinary bridge mode.
// Only the integer guest certificate permits omitted FP switching. Memory base and
// engine stack top remain live; every transfer retains the existing assembly
// root/trap checks and a normal rooted Go callback frame.
func (p *PreparedScalarHost) CallInteger(code, linMem uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	if p == nil || p.engine == nil || p.mode != 2 || linMem == 0 || context == nil || fixed == nil {
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
	if outcome := inlineHostIntegerContextEnter(fixed, context, code, slicePtr(p.args), linMem, slicePtr(p.trap), slicePtr(p.results), p.engine.stackTop, slicePtr(p.ctrl), p.slots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(p.trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), p.trap)
	}
	return nil
}

// CallIntegerView retains result clearing and control/root/trap checks. The
// integer grant certifies the single immutable import signature, so the view
// owner need not compare that shape again on each callback.
func (p *PreparedScalarHost) CallIntegerView(code, linMem uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	if p == nil || p.engine == nil || p.mode != 3 || linMem == 0 || context == nil || fixed == nil {
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
	if outcome := inlineHostIntegerViewContextEnter(fixed, context, code, slicePtr(p.args), linMem, slicePtr(p.trap), slicePtr(p.results), p.engine.stackTop, slicePtr(p.ctrl), p.slots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(p.trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), p.trap)
	}
	return nil
}

func (p *PreparedScalarHost) CallIntegerI32(code, linMem uintptr, fixed func(int32) int32) error {
	if p == nil || p.engine == nil || (p.mode != 2 || p.slots != 0x10001) || linMem == 0 || fixed == nil {
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
	}()
	if outcome := inlineHostIntegerI32Enter(fixed, code, slicePtr(p.args), linMem, slicePtr(p.trap), slicePtr(p.results), p.engine.stackTop, slicePtr(p.ctrl), p.slots); outcome == 1 {
		return fmt.Errorf("jit: inline host escaped admitted import")
	}
	if tc := loadTrap(p.trap); tc != 0 {
		return trapErrorFromBuffer(TrapCode(tc), p.trap)
	}
	return nil
}
