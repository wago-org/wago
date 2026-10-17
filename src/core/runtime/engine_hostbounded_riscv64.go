//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	"github.com/wago-org/wago/internal/runtimebridge"
	"unsafe"
)

const HostCtrlImportIndexOffset = hcImportIdx

type FixedScalarHostContextCall func(unsafe.Pointer, uint64, uint64) uint64
type FixedHostContextCallView func(unsafe.Pointer, []uint64, []uint64)

// Bounded RV64 calls retain the qualified cooperative park/resume ABI.
// The caller proves a bound for each native segment, not the complete call.
func (p *PreparedHostScalarCall) EnableBoundedSegments(access runtimebridge.HostScalarCallAccess) error {
	if !access.Granted() || p == nil || p.engine == nil || !p.fixed || p.fixedSlots>>16 > 2 || p.fixedSlots&0xffff > 2 {
		return fmt.Errorf("jit: invalid bounded host segment admission")
	}
	p.boundedSegments = true
	return nil
}
func (e *Engine) CallWithHostBaseScalarBounded(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedScalarHostCall) error {
	if !access.Granted() || fixed == nil || slots>>16 > 2 || slots&0xffff > 2 {
		return fmt.Errorf("jit: invalid bounded host segment admission")
	}
	return e.callRV64BoundedHost(code, args, home, trap, results, ctrl, rv64BoundedPortal{slots: slots, scalar: fixed}, nil, true)
}
func (e *Engine) callWithBoundedHostLoop(code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedScalarHostCall) error {
	return e.callRV64BoundedHost(code, args, home, trap, results, ctrl, rv64BoundedPortal{slots: slots, scalar: fixed}, nil, false)
}
func (e *Engine) CallWithHostBaseFixedViewBounded(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedHostCallView) error {
	if !access.Granted() || fixed == nil || slots>>16 > maxHostArity || slots&0xffff > maxHostArity {
		return fmt.Errorf("jit: invalid bounded host view admission")
	}
	return e.callRV64BoundedHost(code, args, home, trap, results, ctrl, rv64BoundedPortal{slots: slots, view: fixed}, nil, true)
}
func (e *Engine) CallWithHostBaseMultiViewBounded(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, signatures []uint32, fixed FixedHostCallView) error {
	if !access.Granted() || fixed == nil || len(signatures) < 2 || len(signatures) > 64 {
		return fmt.Errorf("jit: invalid bounded multi-host admission")
	}
	for _, slots := range signatures {
		if slots>>16 > maxHostArity || slots&0xffff > maxHostArity {
			return fmt.Errorf("jit: invalid bounded multi-host shape")
		}
	}
	return e.callRV64BoundedHost(code, args, home, trap, results, ctrl, rv64BoundedPortal{signatures: signatures, view: fixed}, nil, true)
}

// Live API entry uses the cooperative transition until a direct Go bridge has
// its own RV64 stack-map and register proof. Callback semantics are unchanged.
func (e *Engine) CallWithHostBaseScalarBoundedLive(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedScalarHostCall) error {
	return e.CallWithHostBaseScalarBounded(access, code, args, home, trap, results, ctrl, slots, fixed)
}
func (e *Engine) CallWithHostBaseFixedViewBoundedLive(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedHostCallView) error {
	return e.CallWithHostBaseFixedViewBounded(access, code, args, home, trap, results, ctrl, slots, fixed)
}
func (e *Engine) CallWithHostBaseScalarBoundedContextLive(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, context unsafe.Pointer, fixed FixedScalarHostContextCall, fallback FixedScalarHostCall) error {
	if fixed == nil || context == nil {
		return e.CallWithHostBaseScalarBounded(access, code, args, home, trap, results, ctrl, slots, fallback)
	}
	if !access.Granted() || slots>>16 > 2 || slots&0xffff > 2 {
		return fmt.Errorf("jit: invalid bounded host segment admission")
	}
	return e.callRV64BoundedHost(code, args, home, trap, results, ctrl, rv64BoundedPortal{slots: slots, contextScalar: fixed}, context, true)
}
func (e *Engine) CallWithHostBaseFixedViewBoundedContextLive(access runtimebridge.HostScalarCallAccess, code uintptr, args []byte, home uintptr, trap, results, ctrl []byte, slots uint32, context unsafe.Pointer, fixed FixedHostContextCallView, fallback FixedHostCallView) error {
	if fixed == nil || context == nil {
		return e.CallWithHostBaseFixedViewBounded(access, code, args, home, trap, results, ctrl, slots, fallback)
	}
	if !access.Granted() || slots>>16 > maxHostArity || slots&0xffff > maxHostArity {
		return fmt.Errorf("jit: invalid bounded host view admission")
	}
	return e.callRV64BoundedHost(code, args, home, trap, results, ctrl, rv64BoundedPortal{slots: slots, contextView: fixed}, context, true)
}
