//go:build (linux || darwin) && arm64

package runtime

import (
	"encoding/binary"
	"sync"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Synchronous host-import re-entry on arm64 uses the same public control-frame
// constants as amd64, with the arm64 resume stub restoring this frame.
const maxHostArity = 16

const arm64TrapHandlerPtrOffset = 32 // reuses runtimePtr basedata slot; 16 overlaps max-pages cache

const (
	hcSavedSP     = 0
	hcSavedX19    = 8
	hcSavedX20    = 16
	hcSavedX21    = 24
	hcSavedX22    = 32
	hcSavedX23    = 40
	hcSavedX24    = 48
	hcSavedX25    = 56
	hcSavedX26    = 64
	hcSavedX27    = 72
	hcSavedLinMem = 80
	hcSavedFP     = 88
	hcSavedLR     = 96
	hcSavedV8     = 104
	hcSavedV9     = 112
	hcSavedV10    = 120
	hcSavedV11    = 128
	hcSavedV12    = 136
	hcSavedV13    = 144
	hcSavedV14    = 152
	hcSavedV15    = 160

	hcTrampoline  = 176
	hcImportIdx   = 184
	hcNArgs       = 188 // u32: low 16 bits = param slots, high 16 bits = result slots
	hcArgs        = 192
	hcResults     = hcArgs + maxHostArity*8
	ctrlFrameSize = hcResults + maxHostArity*8
)

const hostCallPending = 0x10000

const HostCtrlFrameBytes = ctrlFrameSize
const MaxHostArity = maxHostArity

type HostCall func(ctrl uintptr, importIdx uint32, args, results []uint64)

var (
	hostStubOnce sync.Once
	hostStubPtr  uintptr
	hostStubErr  error
)

func prepareHostResume(ctrl, trap []byte, _ uintptr, stackLimit uintptr) {
	linMem := uintptr(binary.LittleEndian.Uint64(ctrl[hcSavedLinMem:]))
	storeOffHeapU64(linMem-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	storeOffHeapU64(linMem-offStackFence, uint64(stackLimit))
	// resumeNative reinstalls the ARM64 trap re-entry SP and continuation PC.
}

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
	var a a64.Asm
	a.AddImm64(a64.X10, a64.SP, 0)
	a.SubImm64(a64.X9, a64.X26, offCustomCtx)
	mustEncode(a.Load64(a64.X9, a64.X9, 0))
	mustEncode(a.Store64(a64.X10, a64.X9, hcSavedSP))
	mustEncode(a.Store64(a64.X19, a64.X9, hcSavedX19))
	mustEncode(a.Store64(a64.X20, a64.X9, hcSavedX20))
	mustEncode(a.Store64(a64.X21, a64.X9, hcSavedX21))
	mustEncode(a.Store64(a64.X22, a64.X9, hcSavedX22))
	mustEncode(a.Store64(a64.X23, a64.X9, hcSavedX23))
	mustEncode(a.Store64(a64.X24, a64.X9, hcSavedX24))
	mustEncode(a.Store64(a64.X25, a64.X9, hcSavedX25))
	mustEncode(a.Store64(a64.X26, a64.X9, hcSavedX26))
	mustEncode(a.Store64(a64.X27, a64.X9, hcSavedX27))
	mustEncode(a.Store64(a64.X26, a64.X9, hcSavedLinMem))
	mustEncode(a.Store64(a64.FP, a64.X9, hcSavedFP))
	mustEncode(a.Store64(a64.LR, a64.X9, hcSavedLR))
	a.StrF(a64.X9, hcSavedV8, a64.Reg(8), true)
	a.StrF(a64.X9, hcSavedV9, a64.Reg(9), true)
	a.StrF(a64.X9, hcSavedV10, a64.Reg(10), true)
	a.StrF(a64.X9, hcSavedV11, a64.Reg(11), true)
	a.StrF(a64.X9, hcSavedV12, a64.Reg(12), true)
	a.StrF(a64.X9, hcSavedV13, a64.Reg(13), true)
	a.StrF(a64.X9, hcSavedV14, a64.Reg(14), true)
	a.StrF(a64.X9, hcSavedV15, a64.Reg(15), true)
	a.SubImm64(a64.X10, a64.X26, abi.TrapCellPtrOffset)
	mustEncode(a.Load64(a64.X10, a64.X10, 0))
	mustEncode(a.Store64(a64.X9, a64.X10, 8)) // publish the exact active control frame at trap+8
	a.MovImm64(a64.X11, hostCallPending)
	mustEncode(a.Store32(a64.X11, a64.X10, 0))
	a.SubImm64(a64.X10, a64.X26, offTrapStackReentry)
	mustEncode(a.Load64(a64.X10, a64.X10, 0))
	a.SubImm64(a64.LR, a64.X26, arm64TrapHandlerPtrOffset)
	mustEncode(a.Load64(a64.LR, a64.LR, 0))
	a.AddImm64(a64.SP, a64.X10, 0)
	a.Ret()
	return a.B
}

func mustEncode(ok bool) {
	if !ok {
		panic("arm64 host-call stub encoding failed")
	}
}
