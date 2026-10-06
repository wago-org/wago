//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/abi"
	goruntime "runtime"
	"unsafe"
)

// IntegerI32Owner is sealed immutable admission data for a serialized owner.
// The caller must retain the ordinary instance/session ownership and cleanup.
type IntegerI32Owner struct {
	prepared                         *PreparedScalarHost
	trampoline                       *uint64
	base                             uintptr
	bridge, staged                   uint64
	args, trap, results, stack, ctrl uintptr
	slots                            uint32
}

func (p *PreparedScalarHost) IntegerI32Owner() *IntegerI32Owner {
	if p == nil || p.engine == nil || p.mode != 2 || p.slots != 0x10001 || p.detached == nil {
		return nil
	}
	return &IntegerI32Owner{prepared: p, trampoline: (*uint64)(unsafe.Pointer(&p.ctrl[hcTrampoline])), base: p.detachedBase(), bridge: uint64(p.bridge), staged: uint64(p.staged), args: slicePtr(p.args), trap: slicePtr(p.trap), results: slicePtr(p.results), stack: p.engine.stackTop, ctrl: slicePtr(p.ctrl), slots: p.slots}
}
func (o *IntegerI32Owner) Prepared() *PreparedScalarHost { return o.prepared }
func (o *IntegerI32Owner) Begin() {
	clearTrapUnlessInterrupted(o.prepared.trap)
	storeOffHeapU64(o.base-abi.TrapCellPtrOffset, uint64(slicePtr(o.prepared.trap)))
	*o.trampoline = o.bridge
}
func (o *IntegerI32Owner) Restore() { *o.trampoline = o.staged; goruntime.KeepAlive(o.prepared) }
func (o *IntegerI32Owner) Enter(code uintptr, fixed func(int32) int32) uint64 {
	return inlineHostIntegerI32Enter(fixed, code, o.args, o.base, o.trap, o.results, o.stack, o.ctrl, o.slots)
}
func (o *IntegerI32Owner) TrapCode() uint32 { return loadTrap(o.prepared.trap) }
func (o *IntegerI32Owner) TrapError(code uint32) error {
	return trapErrorFromBuffer(TrapCode(code), o.prepared.trap)
}
func (o *IntegerI32Owner) EscapedError() error {
	return fmt.Errorf("jit: inline host escaped admitted import")
}
