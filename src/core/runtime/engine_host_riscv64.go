//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import "github.com/wago-org/wago/internal/runtimebridge"

// Parked-host state/entry is unavailable until the RV64 save/resume ABI exists.
// All entry variants reject before callbacks, native entry or control mutation.
type engineHostState struct{}
type HostCall func(ctrl uintptr, importIdx uint32, args, results []uint64)
type ScalarHostCall func(ctrl uintptr, importIdx, rawSlots uint32, a0, a1 uint64) (uint64, bool)
type FixedScalarHostCall func(a0, a1 uint64) uint64
type PreparedHostScalarCall struct{}

func (*Engine) CallWithHost(uintptr, []byte, []byte, []byte, []byte, []byte, HostCall) error {
	return ErrUnsupported
}
func (*Engine) CallWithHostBase(uintptr, []byte, uintptr, []byte, []byte, []byte, HostCall) error {
	return ErrUnsupported
}
func (*Engine) CallWithHostBaseScalar(uintptr, []byte, uintptr, []byte, []byte, []byte, HostCall, ScalarHostCall) error {
	return ErrUnsupported
}
func (*Engine) CallWithHostBaseScalarExpanded(uintptr, []byte, uintptr, []byte, []byte, []byte, HostCall, ScalarHostCall) error {
	return ErrUnsupported
}
func (*Engine) PrepareHostScalarCall(runtimebridge.HostScalarCallAccess, uintptr, []byte, *JobMemory, []byte, []byte, []byte) (*PreparedHostScalarCall, error) {
	return nil, ErrUnsupported
}
func (*Engine) PrepareHostScalarFixedCall(runtimebridge.HostScalarCallAccess, uintptr, []byte, *JobMemory, []byte, []byte, []byte, uint32) (*PreparedHostScalarCall, error) {
	return nil, ErrUnsupported
}
func (*PreparedHostScalarCall) Call(HostCall, ScalarHostCall) error { return ErrUnsupported }
func (*PreparedHostScalarCall) CallFixed(HostCall, ScalarHostCall, FixedScalarHostCall) error {
	return ErrUnsupported
}
func InitHostCtrlFrame([]byte) error { return ErrUnsupported }
