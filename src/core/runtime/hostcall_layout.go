//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
)

// MaxSyncHostSlots is the wire-format ceiling: hcNArgs carries each direction's
// slot count in one uint16. MaxHostArity remains the public-host admission limit;
// this larger limit is for module-derived internal helper frames.
const MaxSyncHostSlots = math.MaxUint16

const (
	hostCtrlExtensionMagic = 0x5741474f // "WAGO"
	hostCtrlExtensionHead  = 8          // magic u32, capacity u32
)

// HostCtrlFrameBytesForSlots returns the control-frame bytes for a symmetric
// param/result capacity. The common <=64-slot frame is unchanged; a wide frame
// appends full argument and result areas. Calls that fit the inline frame keep
// using the original offsets, even when the module owns a wide frame.
func HostCtrlFrameBytesForSlots(slots int) (int, error) {
	if slots < 0 || slots > MaxSyncHostSlots {
		return 0, fmt.Errorf("jit: synchronous host slot capacity %d is outside 0..%d", slots, MaxSyncHostSlots)
	}
	if slots <= maxHostArity {
		return ctrlFrameSize, nil
	}
	return ctrlFrameSize + hostCtrlExtensionHead + slots*16, nil
}

func initHostCtrlExtension(ctrl []byte) (int, error) {
	if len(ctrl) == ctrlFrameSize {
		return maxHostArity, nil
	}
	extra := len(ctrl) - ctrlFrameSize - hostCtrlExtensionHead
	if extra <= 0 || extra%16 != 0 {
		return 0, fmt.Errorf("jit: host control frame has invalid extended size %d", len(ctrl))
	}
	capacity := extra / 16
	if capacity <= maxHostArity || capacity > MaxSyncHostSlots {
		return 0, fmt.Errorf("jit: host control frame has invalid extended capacity %d", capacity)
	}
	ext := ctrl[ctrlFrameSize:]
	binary.LittleEndian.PutUint32(ext, hostCtrlExtensionMagic)
	binary.LittleEndian.PutUint32(ext[4:], uint32(capacity))
	return capacity, nil
}

var registeredHostCtrlFrames sync.Map // map[uintptr]int; lengths only, never Go pointers

// RegisterHostCtrlFrame publishes an independently known control-frame length
// for cross-instance parked calls. CallWithHostBase already knows its root frame
// length directly.
func RegisterHostCtrlFrame(ctrl []byte) error {
	if _, err := hostCtrlFrameCapacity(ctrl); err != nil {
		return err
	}
	ptr := slicePtr(ctrl)
	if ptr == 0 {
		return fmt.Errorf("jit: host control frame is empty")
	}
	if _, loaded := registeredHostCtrlFrames.LoadOrStore(ptr, len(ctrl)); loaded {
		return fmt.Errorf("jit: host control frame %#x is already registered", ptr)
	}
	return nil
}

// UnregisterHostCtrlFrame removes a frame registered at instance activation.
func UnregisterHostCtrlFrame(ctrl []byte) {
	if ptr := slicePtr(ctrl); ptr != 0 {
		registeredHostCtrlFrames.Delete(ptr)
	}
}

func hostCtrlFrameCapacity(ctrl []byte) (int, error) {
	if len(ctrl) == ctrlFrameSize {
		return maxHostArity, nil
	}
	if len(ctrl) < ctrlFrameSize+hostCtrlExtensionHead {
		return 0, fmt.Errorf("jit: host control frame has invalid size %d", len(ctrl))
	}
	head := ctrl[ctrlFrameSize : ctrlFrameSize+hostCtrlExtensionHead]
	if binary.LittleEndian.Uint32(head) != hostCtrlExtensionMagic {
		return 0, fmt.Errorf("jit: host control extension has invalid magic")
	}
	capacity := int(binary.LittleEndian.Uint32(head[4:]))
	if capacity <= maxHostArity || capacity > MaxSyncHostSlots {
		return 0, fmt.Errorf("jit: host control extension has invalid capacity %d", capacity)
	}
	want := ctrlFrameSize + hostCtrlExtensionHead + capacity*16
	if len(ctrl) != want {
		return 0, fmt.Errorf("jit: host control frame has %d bytes, capacity %d requires %d", len(ctrl), capacity, want)
	}
	return capacity, nil
}

// hostCtrlWideCallAreas returns checked appended native/Go exchange areas.
func hostCtrlWideCallAreas(ctrl []byte, paramSlots, resultSlots int) (args, results []byte, capacity int, err error) {
	capacity, err = hostCtrlFrameCapacity(ctrl)
	if err != nil {
		return nil, nil, 0, err
	}
	if paramSlots > capacity || resultSlots > capacity {
		return nil, nil, 0, fmt.Errorf("jit: host call arity %d/%d exceeds extended capacity %d", paramSlots, resultSlots, capacity)
	}
	ext := ctrl[ctrlFrameSize+hostCtrlExtensionHead:]
	return ext[:capacity*8], ext[capacity*8:], capacity, nil
}
