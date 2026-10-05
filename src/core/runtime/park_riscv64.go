//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// This private runtime proof is separate from the admitted host control ABI.
// It stores guest state only: gp/tp/CTXT/g must belong to the current Go thread
// and are freshly saved by each resume bridge, never restored from a park.
type rv64ParkFrame struct {
	SP, RA                                                   uint64
	S0, S1, S2, S3, S4, S5, S6, S7, S8, S9                   uint64
	F8, F9, F18, F19, F20, F21, F22, F23, F24, F25, F26, F27 uint64
	FCSR, BridgeSP                                           uint64
}

const rv64ParkFrameBytes = unsafe.Sizeof(rv64ParkFrame{})
const rv64ParkPending = 0x10000

func rv64ParkStub() ([]byte, error) {
	var a enc.Asm
	a.Ld(enc.X28, enc.X25, -offCustomCtx)
	a.Sd(enc.SP, enc.X28, int32(unsafe.Offsetof(rv64ParkFrame{}.SP)))
	a.Sd(enc.RA, enc.X28, int32(unsafe.Offsetof(rv64ParkFrame{}.RA)))
	for i, r := range [...]enc.Reg{enc.X8, enc.X9, enc.X18, enc.X19, enc.X20, enc.X21, enc.X22, enc.X23, enc.X24, enc.X25} {
		a.Sd(r, enc.X28, int32(unsafe.Offsetof(rv64ParkFrame{}.S0))+int32(i*8))
	}
	for i, r := range [...]enc.FReg{enc.F8, enc.F9, enc.F18, enc.F19, enc.F20, enc.F21, enc.F22, enc.F23, enc.F24, enc.F25, enc.F26, enc.F27} {
		a.FmvXD(enc.X5, r)
		a.Sd(enc.X5, enc.X28, int32(unsafe.Offsetof(rv64ParkFrame{}.F8))+int32(i*8))
	}
	a.ReadFcsr(enc.X5)
	a.Sd(enc.X5, enc.X28, int32(unsafe.Offsetof(rv64ParkFrame{}.FCSR)))
	// Normal return retains the original activation's bridge continuation and
	// stack position. Resume must rebuild its Go save area at this exact SP.
	a.Ld(enc.X5, enc.X25, -int32(RV64TrapReentrySPOffset))
	a.Sd(enc.X5, enc.X28, int32(unsafe.Offsetof(rv64ParkFrame{}.BridgeSP)))
	a.Ld(enc.X29, enc.X25, -int32(abi.TrapCellPtrOffset))
	a.Sd(enc.X28, enc.X29, 8)
	a.Constant64(enc.X30, rv64ParkPending)
	// A constrained four-instruction LR/SC envelope. No veneer or extra memory
	// operation may be inserted between LR and SC; every nonzero SC retries.
	a.LrW(enc.X5, enc.X29, enc.AtomicAcquire)
	a.Branch(enc.NE, enc.X5, enc.Zero, 12)
	a.ScW(enc.X6, enc.X30, enc.X29, enc.AtomicRelease)
	a.Branch(enc.NE, enc.X6, enc.Zero, -12)
	a.Ld(enc.SP, enc.X25, -int32(RV64TrapReentrySPOffset))
	a.Ld(enc.X31, enc.X25, -int32(RV64TrapContinuationOffset))
	a.Jalr(enc.Zero, enc.X31, 0)
	return a.B, a.Err()
}

//go:noescape
func resumeNativeRV64Raw(ctrl, trap, stack uintptr)

//go:nosplit
func resumeNativeRV64(ctrl, trap, stack uintptr) {
	nativeEnterSyscall()
	resumeNativeRV64Raw(ctrl, trap, stack)
	nativeExitSyscall()
}
