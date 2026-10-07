//go:build (linux || darwin || windows) && (amd64 || arm64) && (windows || tinygo)

package runtime

import (
	"fmt"
	"unsafe"
)

func preparedScalarHostAvailable() bool     { return false }
func preparedScalarHostBridge() uintptr     { return 0 }
func preparedScalarHostViewBridge() uintptr { return 0 }

func (p *PreparedScalarHost) Call(code, linMem uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	return fmt.Errorf("jit: prepared scalar host bridge unavailable")
}

func (p *PreparedScalarHost) CallView(code, linMem uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	return fmt.Errorf("jit: prepared scalar host view unavailable")
}
