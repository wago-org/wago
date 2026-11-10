//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
)

// Malformed frames must fail before touching the supplied foreign addresses.
func TestLiveBoundedEntryRejectsMalformedCtrlBeforeNativeEntry(t *testing.T) {
	for _, n := range []int{0, ctrlFrameSize - 1, ctrlFrameSize + 1, ctrlFrameSize + hostCtrlExtensionHead + 16, ctrlFrameSize + hostCtrlExtensionHead + (MaxSyncHostSlots+1)*16} {
		e := &Engine{}
		err := e.CallWithHostBaseScalarBoundedLive(runtimebridge.GrantHostScalarCall(), 1, nil, 1, make([]byte, TrapBufferBytes), nil, make([]byte, n), 1|1<<16, func(uint64, uint64) uint64 { t.Fatal("callback ran"); return 0 })
		if err == nil || !strings.Contains(err.Error(), "host control frame") {
			t.Fatalf("size %d: %v", n, err)
		}
	}
}

func TestLiveBoundedViewRejectsMalformedCtrlBeforeNativeEntry(t *testing.T) {
	for _, n := range []int{0, ctrlFrameSize - 1, ctrlFrameSize + 1, ctrlFrameSize + hostCtrlExtensionHead + 16, ctrlFrameSize + hostCtrlExtensionHead + (MaxSyncHostSlots+1)*16} {
		e := &Engine{}
		err := e.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), 1, nil, 1, make([]byte, TrapBufferBytes), nil, make([]byte, n), 1|1<<16, func([]uint64, []uint64) { t.Fatal("callback ran") })
		if err == nil || !strings.Contains(err.Error(), "host control frame") {
			t.Fatalf("size %d: %v", n, err)
		}
	}
}

func TestLiveBoundedContextRejectsMalformedCtrlBeforeNativeEntry(t *testing.T) {
	for _, n := range []int{0, ctrlFrameSize - 1, ctrlFrameSize + 1, ctrlFrameSize + hostCtrlExtensionHead + 16, ctrlFrameSize + hostCtrlExtensionHead + (MaxSyncHostSlots+1)*16} {
		e := &Engine{}
		context := new(byte)
		err := e.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), 1, nil, 1, make([]byte, TrapBufferBytes), nil, make([]byte, n), 1|1<<16, unsafe.Pointer(context), func(unsafe.Pointer, uint64, uint64) uint64 { t.Fatal("callback ran"); return 0 }, func(uint64, uint64) uint64 { t.Fatal("fallback ran"); return 0 })
		if err == nil || !strings.Contains(err.Error(), "host control frame") {
			t.Fatalf("size %d: %v", n, err)
		}
	}
}

func TestLiveBoundedViewContextRejectsMalformedCtrlBeforeNativeEntry(t *testing.T) {
	for _, n := range []int{0, ctrlFrameSize - 1, ctrlFrameSize + 1, ctrlFrameSize + hostCtrlExtensionHead + 16, ctrlFrameSize + hostCtrlExtensionHead + (MaxSyncHostSlots+1)*16} {
		e := &Engine{}
		context := new(byte)
		err := e.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), 1, nil, 1, make([]byte, TrapBufferBytes), nil, make([]byte, n), 1|1<<16, unsafe.Pointer(context), func(unsafe.Pointer, []uint64, []uint64) { t.Fatal("callback ran") }, func([]uint64, []uint64) { t.Fatal("fallback ran") })
		if err == nil || !strings.Contains(err.Error(), "host control frame") {
			t.Fatalf("size %d: %v", n, err)
		}
	}
}
