//go:build (linux || darwin || windows) && (amd64 || arm64) && (windows || tinygo)

package runtime

import (
	"fmt"
	"unsafe"
)

func (p *PreparedScalarHost) CallInteger(code, linMem uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	return fmt.Errorf("jit: prepared scalar host bridge unavailable")
}

func (p *PreparedScalarHost) CallIntegerView(code, linMem uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	return fmt.Errorf("jit: prepared scalar host view unavailable")
}

func (p *PreparedScalarHost) CallIntegerI32(code, linMem uintptr, fixed func(int32) int32) error {
	return fmt.Errorf("jit: prepared integer i32 host bridge unavailable")
}
