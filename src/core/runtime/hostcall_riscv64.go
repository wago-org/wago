//go:build linux && riscv64

package runtime

import (
	"sync"

	rv "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const maxHostArity = 16
const riscv64TrapHandlerPtrOffset = 32

// Keep these public protocol fields layout-compatible with the arm64 backend so
// instance/control-frame allocation remains architecture-neutral. The reserved
// [104,176) region is unused on RV64 because the psABI has no callee-saved FPRs.
const (
	hcSavedSP     = 0
	hcSavedX19    = 8  // S0
	hcSavedX20    = 16 // S1
	hcSavedX21    = 24 // S2
	hcSavedX22    = 32 // S3
	hcSavedX23    = 40 // S4
	hcSavedX24    = 48 // S5
	hcSavedX25    = 56 // S6
	hcSavedX26    = 64 // S7
	hcSavedX27    = 72 // S8
	hcSavedLinMem = 80 // S9
	hcSavedFP     = 88 // reserved/duplicate S0 for layout parity
	hcSavedLR     = 96 // RA

	hcTrampoline  = 176
	hcImportIdx   = 184
	hcNArgs       = 188
	hcArgs        = 192
	hcResults     = hcArgs + maxHostArity*8
	ctrlFrameSize = hcResults + maxHostArity*8
)

const hostCallPending = 0x10000
const HostCtrlFrameBytes = ctrlFrameSize
const MaxHostArity = maxHostArity

type HostCall func(importIdx uint32, args, results []uint64)

var (
	hostStubOnce sync.Once
	hostStubPtr  uintptr
	hostStubErr  error
)

func hostCallStubPtr() (uintptr, error) {
	hostStubOnce.Do(func() {
		mem, err := mmapExec(hostCallStub())
		if err != nil {
			hostStubErr = err
			return
		}
		hostStubPtr = slicePtr(mem)
	})
	return hostStubPtr, hostStubErr
}

func hostCallStub() []byte {
	var a rv.Asm
	// T0 = control frame at [S9-offCustomCtx].
	mustRV(a.Addi(rv.T0, rv.S9, -offCustomCtx))
	mustRV(a.Ld(rv.T0, rv.T0, 0))
	mustRV(a.Sd(rv.SP, rv.T0, hcSavedSP))
	mustRV(a.Sd(rv.S0, rv.T0, hcSavedX19))
	mustRV(a.Sd(rv.S1, rv.T0, hcSavedX20))
	mustRV(a.Sd(rv.S2, rv.T0, hcSavedX21))
	mustRV(a.Sd(rv.S3, rv.T0, hcSavedX22))
	mustRV(a.Sd(rv.S4, rv.T0, hcSavedX23))
	mustRV(a.Sd(rv.S5, rv.T0, hcSavedX24))
	mustRV(a.Sd(rv.S6, rv.T0, hcSavedX25))
	mustRV(a.Sd(rv.S7, rv.T0, hcSavedX26))
	mustRV(a.Sd(rv.S8, rv.T0, hcSavedX27))
	mustRV(a.Sd(rv.S9, rv.T0, hcSavedLinMem))
	mustRV(a.Sd(rv.S0, rv.T0, hcSavedFP))
	mustRV(a.Sd(rv.RA, rv.T0, hcSavedLR))

	// Publish hostCallPending through basedata's trap-cell pointer.
	mustRV(a.Addi(rv.T1, rv.S9, -abi.TrapCellPtrOffset))
	mustRV(a.Ld(rv.T1, rv.T1, 0))
	a.MovImm64(rv.T2, hostCallPending)
	mustRV(a.Sw(rv.T2, rv.T1, 0))

	// Unwind to the Go continuation recorded by enterNative/resumeNative.
	mustRV(a.Addi(rv.T1, rv.S9, -offTrapStackReentry))
	mustRV(a.Ld(rv.T1, rv.T1, 0))
	mustRV(a.Addi(rv.T2, rv.S9, -riscv64TrapHandlerPtrOffset))
	mustRV(a.Ld(rv.T2, rv.T2, 0))
	mustRV(a.Addi(rv.SP, rv.T1, 0))
	a.Jalr(rv.Zero, rv.T2, 0)
	return a.B
}

func mustRV(ok bool) {
	if !ok {
		panic("riscv64 host-call stub encoding failed")
	}
}
