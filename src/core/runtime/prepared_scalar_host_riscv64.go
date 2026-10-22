//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	"os"
	"unsafe"
)

var preparedScalarHostEnabled = os.Getenv("WAGO_NO_PREPARED_SCALAR_HOST") != "1"

func preparedScalarHostAvailable() bool     { return preparedScalarHostEnabled }
func preparedScalarHostBridge() uintptr     { ptr, _ := hostCallStubPtr(); return ptr }
func preparedScalarHostViewBridge() uintptr { return preparedScalarHostBridge() }
func integerHostContextBridge(bool) uintptr { return preparedScalarHostBridge() }
func (p *PreparedScalarHost) callRV64(code, home uintptr, context unsafe.Pointer, scalar FixedScalarHostContextCall, view FixedHostContextCallView, i32 func(int32) int32, wantMode uint8) error {
	if p == nil || p.engine == nil || p.engine.preparedScalarHost != p || p.mode != wantMode || home == 0 || code == 0 || scalar == nil && view == nil && i32 == nil || i32 == nil && context == nil {
		return fmt.Errorf("jit: invalid prepared scalar host call")
	}
	if p.detached != nil {
		home = p.detachedBase()
	}
	return p.engine.callRV64BoundedHost(code, p.args, home, p.trap, p.results, p.ctrl, rv64BoundedPortal{slots: p.slots, contextScalar: scalar, contextView: view, i32: i32}, context, false)
}
func (p *PreparedScalarHost) Call(code, home uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	return p.callRV64(code, home, context, fixed, nil, nil, 0)
}
func (p *PreparedScalarHost) CallView(code, home uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	return p.callRV64(code, home, context, nil, fixed, nil, 1)
}
func (p *PreparedScalarHost) CallInteger(code, home uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	return p.callRV64(code, home, context, fixed, nil, nil, 2)
}
func (p *PreparedScalarHost) CallIntegerView(code, home uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	return p.callRV64(code, home, context, nil, fixed, nil, 3)
}
func (p *PreparedScalarHost) CallIntegerI32(code, home uintptr, fixed func(int32) int32) error {
	if p == nil || p.slots != 0x10001 {
		return fmt.Errorf("jit: invalid prepared integer i32 host call")
	}
	return p.callRV64(code, home, nil, nil, nil, fixed, 2)
}
