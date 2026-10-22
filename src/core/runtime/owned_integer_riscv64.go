//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/abi"
	goruntime "runtime"
	"unsafe"
)

// IntegerI32Owner is immutable admission metadata; its ordinary instance or
// session owner still serializes calls and controls prepared-handle lifetime.
type IntegerI32Owner struct {
	prepared                         *PreparedScalarHost
	trampoline                       *uint64
	base                             uintptr
	bridge, staged                   uint64
	args, trap, results, stack, ctrl uintptr
	slots                            uint32
}

func (p *PreparedScalarHost) IntegerI32Owner() *IntegerI32Owner {
	if p == nil || p.engine == nil || p.engine.preparedScalarHost != p || p.mode != 2 || p.slots != 0x10001 || p.detached == nil {
		return nil
	}
	return p.rv64IntegerOwner()
}
func (p *PreparedScalarHost) rv64IntegerOwner() *IntegerI32Owner {
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
	if err := o.prepared.CallIntegerI32(code, o.base, fixed); err != nil && loadTrap(o.prepared.trap) == 0 {
		return 1
	}
	return 0
}
func (o *IntegerI32Owner) TrapCode() uint32 { return loadTrap(o.prepared.trap) }
func (o *IntegerI32Owner) TrapError(code uint32) error {
	return trapErrorFromBuffer(TrapCode(code), o.prepared.trap)
}
func (o *IntegerI32Owner) EscapedError() error {
	return fmt.Errorf("jit: RV64 host escaped admitted import")
}

type IntegerViewOwner struct{ IntegerI32Owner }

func (p *PreparedScalarHost) IntegerViewOwner() *IntegerViewOwner {
	if p == nil || p.engine == nil || p.engine.preparedScalarHost != p || p.mode != 3 || p.detached == nil {
		return nil
	}
	return &IntegerViewOwner{*p.rv64IntegerOwner()}
}
func (o *IntegerViewOwner) EnterView(code uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) uint64 {
	if err := o.prepared.CallIntegerView(code, o.base, context, fixed); err != nil && loadTrap(o.prepared.trap) == 0 {
		return 1
	}
	return 0
}
