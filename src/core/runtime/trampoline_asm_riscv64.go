//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"github.com/wago-org/wago/src/core/runtime/abi"
	"unsafe"
)

// RISC-style continuation uses -32; -16 overlaps the maximum-pages cache.
const RV64TrapContinuationOffset = abi.RV64TrapContinuationOffset
const RV64TrapReentrySPOffset = abi.RV64TrapReentrySPOffset

type nativeWrapperSaveFrame struct {
	GoSP, RA, GP, TP                       uint64
	S0, S1, S2, S3, S4, S5, S6, S7, S8, S9 uint64
	CTXT, G                                uint64
	FCSR, Padding                          uint64
}

const nativeWrapperSaveBytes = unsafe.Sizeof(nativeWrapperSaveFrame{})

// Compile-time layout agreement for generated invocation-view guards.
var _ [abi.RV64NativeWrapperSaveBytes - nativeWrapperSaveBytes]byte
var _ [nativeWrapperSaveBytes - abi.RV64NativeWrapperSaveBytes]byte

// Production wrapper ABI: A0=args,A1=linMem,A2=trap,A3=results,X25=pinned linMem.
func enterNativeRaw(code, serArgs, linMem, trap, results, foreignStackTop uintptr)
func (*Engine) initNativeEntry() error { return nil }
