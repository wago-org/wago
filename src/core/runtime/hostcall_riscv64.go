//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"sync"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The guest prefix is shared with the qualified park/resume bridge. Host wire
// fields start after that complete prefix; no ARM64 or historical offsets apply.
const (
	maxHostArity       = 64
	hcSavedLinMem      = int(unsafe.Offsetof(rv64ParkFrame{}.S9))
	hcTrampoline       = int(rv64ParkFrameBytes)
	hcImportIdx        = hcTrampoline + 8
	hcNArgs            = hcImportIdx + 4
	hcArgs             = hcNArgs + 4
	hcResults          = hcArgs + maxHostArity*8
	ctrlFrameSize      = hcResults + maxHostArity*8
	hostCallPending    = rv64ParkPending
	HostCtrlFrameBytes = ctrlFrameSize
	MaxHostArity       = maxHostArity
)

type HostCall func(ctrl uintptr, importIdx uint32, args, results []uint64)

var (
	hostStubOnce sync.Once
	hostStubPtr  uintptr
	hostStubErr  error
	hostStubMem  []byte // process-lifetime owner, including failed rollback
)

func hostCallStubPtr() (uintptr, error) {
	hostStubOnce.Do(func() {
		code, err := rv64ParkStub()
		if err != nil {
			hostStubErr = err
			return
		}
		mem, err := mmapExec(code)
		hostStubMem = mem
		if err != nil {
			hostStubErr = err
			return
		}
		hostStubPtr = slicePtr(mem)
	})
	return hostStubPtr, hostStubErr
}

func prepareHostResume(ctrl, trap []byte, _ uintptr, stackLimit uintptr) {
	home := uintptr(binary.LittleEndian.Uint64(ctrl[hcSavedLinMem:]))
	storeOffHeapU64(home-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	storeOffHeapU64(home-offStackFence, uint64(stackLimit))
	// Raw resume checks the recorded original BridgeSP, refreshes Go thread state
	// and installs its continuation at -32 without touching max-pages at -12.
}

func resumeNative(ctrl, stack uintptr) {
	home := uintptr(*(*uint64)(offHeapPointer(ctrl + uintptr(hcSavedLinMem))))
	trap := uintptr(*(*uint64)(offHeapPointer(home - abi.TrapCellPtrOffset)))
	resumeNativeRV64(ctrl, trap, stack)
}
