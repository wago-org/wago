//go:build (linux || darwin || windows) && (amd64 || arm64) && (windows || tinygo)

package runtime

import (
	"github.com/wago-org/wago/internal/runtimebridge"
	"unsafe"
)

func (e *Engine) CallWithHostBaseScalarBoundedLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedScalarHostCall) error {
	return e.CallWithHostBaseScalarBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
}

func (e *Engine) CallWithHostBaseFixedViewBoundedLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, fixed FixedHostCallView) error {
	return e.CallWithHostBaseFixedViewBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fixed)
}

func (e *Engine) CallWithHostBaseScalarBoundedContextLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, context unsafe.Pointer, fixed FixedScalarHostContextCall, fallback FixedScalarHostCall) error {
	return e.CallWithHostBaseScalarBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fallback)
}

func (e *Engine) CallWithHostBaseFixedViewBoundedContextLive(access runtimebridge.HostScalarCallAccess, code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, rawSlots uint32, context unsafe.Pointer, fixed FixedHostContextCallView, fallback FixedHostCallView) error {
	return e.CallWithHostBaseFixedViewBounded(access, code, serArgs, linMemBase, trap, results, ctrl, rawSlots, fallback)
}
