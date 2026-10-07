//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"errors"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
)

// RV64 retains its framed, cooperative parked-host protocol. The fresh-code
// bounded/live Go portals have no RV64 ABI qualification or compiler proof bit.
// Keep their common API available without admitting a native entry through it.
var errRV64BoundedHostUnavailable = errors.New("jit: bounded live host entry is unavailable on RV64")

const HostCtrlImportIndexOffset = hcImportIdx

type FixedScalarHostContextCall func(unsafe.Pointer, uint64, uint64) uint64
type FixedHostContextCallView func(unsafe.Pointer, []uint64, []uint64)

func (p *PreparedHostScalarCall) EnableBoundedSegments(runtimebridge.HostScalarCallAccess) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) callWithBoundedHostLoop(uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, FixedScalarHostCall) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseScalarBounded(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, FixedScalarHostCall) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseFixedViewBounded(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, FixedHostCallView) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseMultiViewBounded(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, []uint32, FixedHostCallView) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseScalarBoundedLive(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, FixedScalarHostCall) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseFixedViewBoundedLive(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, FixedHostCallView) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseScalarBoundedContextLive(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, unsafe.Pointer, FixedScalarHostContextCall, FixedScalarHostCall) error {
	return errRV64BoundedHostUnavailable
}

func (e *Engine) CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.HostScalarCallAccess, uintptr, []byte, uintptr, []byte, []byte, []byte, uint32, unsafe.Pointer, FixedHostContextCallView, FixedHostCallView) error {
	return errRV64BoundedHostUnavailable
}
