//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	goruntime "runtime"
	"unsafe"
)

// IntegerViewOwner retains the prepared view bridge's immutable numeric metadata.
// Ordinary session admission and cleanup remain the caller's responsibility.
type IntegerViewOwner struct{ IntegerI32Owner }

func (p *PreparedScalarHost) IntegerViewOwner() *IntegerViewOwner {
	if p == nil || p.engine == nil || p.mode != 3 || p.detached == nil {
		return nil
	}
	return &IntegerViewOwner{IntegerI32Owner{prepared: p, trampoline: (*uint64)(unsafe.Pointer(&p.ctrl[hcTrampoline])), base: p.detachedBase(), bridge: uint64(p.bridge), staged: uint64(p.staged), args: slicePtr(p.args), trap: slicePtr(p.trap), results: slicePtr(p.results), stack: p.engine.stackTop, ctrl: slicePtr(p.ctrl), slots: p.slots}}
}
func (o *IntegerViewOwner) EnterView(code uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) uint64 {
	outcome := inlineHostIntegerViewContextEnter(fixed, context, code, o.args, o.base, o.trap, o.results, o.stack, o.ctrl, o.slots)
	goruntime.KeepAlive(context)
	return outcome
}
