//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"
)

type inlineOnlyEngineLayout struct {
	stack       []byte
	stackTop    uintptr
	_           *PreparedScalarHost // Account for the prepared scalar bridge pointer.
	preparedInt tinygoPreparedIntState
	inUse       bool
	args        [maxHostArity]uint64
	results     [maxHostArity]uint64
}

func TestInstantiateArenaNeedAccountsExplicitHostControlFrame(t *testing.T) {
	base, err := InstantiateArenaNeed(InstantiateFootprint{})
	if err != nil {
		t.Fatal(err)
	}
	withCtrl, err := InstantiateArenaNeed(InstantiateFootprint{HostCallBytes: HostCtrlFrameBytes})
	if err != nil {
		t.Fatal(err)
	}
	if got := withCtrl - base; got != HostCtrlFrameBytes {
		t.Fatalf("explicit host control-frame delta = %d, want %d", got, HostCtrlFrameBytes)
	}
	if _, err := InstantiateArenaNeed(InstantiateFootprint{HostCallBytes: -1}); err == nil {
		t.Fatal("negative host control-frame footprint unexpectedly accepted")
	}
}

func TestHostCtrlFrameWideCapacityKeepsInlineLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(Engine{}), unsafe.Sizeof(inlineOnlyEngineLayout{}); got != want {
		t.Fatalf("Engine size = %d, inline scratch and prepared bridge layout = %d", got, want)
	}
	if got, err := HostCtrlFrameBytesForSlots(maxHostArity); err != nil || got != HostCtrlFrameBytes {
		t.Fatalf("64-slot frame = %d, %v; want %d", got, err, HostCtrlFrameBytes)
	}
	const wide = 404
	got, err := HostCtrlFrameBytesForSlots(wide)
	if err != nil {
		t.Fatal(err)
	}
	want := HostCtrlFrameBytes + hostCtrlExtensionHead + wide*16
	if got != want {
		t.Fatalf("404-slot frame = %d, want %d", got, want)
	}
	if _, err := HostCtrlFrameBytesForSlots(MaxSyncHostSlots + 1); err == nil {
		t.Fatal("capacity above uint16 was accepted")
	}
	ctrl := make([]byte, got)
	const lastInlineResult = uint64(0xfeedfacecafebeef)
	binary.LittleEndian.PutUint64(ctrl[hcResults+(maxHostArity-1)*8:], lastInlineResult)
	if err := InitHostCtrlFrame(ctrl); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(ctrl[hcResults+(maxHostArity-1)*8:]); got != lastInlineResult {
		t.Fatalf("wide-frame initialization retained native pointer in inline result slot: %#x", got)
	}
	args, results, capacity, err := hostCtrlWideCallAreas(ctrl, wide, 1)
	if err != nil {
		t.Fatal(err)
	}
	if capacity != wide || len(args) != wide*8 || len(results) != wide*8 {
		t.Fatalf("extended areas = capacity %d, bytes %d/%d", capacity, len(args), len(results))
	}
	binary.LittleEndian.PutUint32(ctrl[HostCtrlFrameBytes+4:], wide+1)
	if _, _, _, err := hostCtrlWideCallAreas(ctrl, wide, 1); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("mismatched extension layout error = %v", err)
	}
}

func TestHostCtrlFrameRegistrationOnlyTracksWideLengths(t *testing.T) {
	inline := make([]byte, HostCtrlFrameBytes)
	if err := InitHostCtrlFrame(inline); err != nil {
		t.Fatal(err)
	}
	if err := RegisterHostCtrlFrame(inline); err != nil {
		t.Fatal(err)
	}
	inlinePtr := slicePtr(inline)
	if _, loaded := registeredHostCtrlFrames.Load(inlinePtr); loaded {
		t.Fatal("ordinary control frame acquired a length-registry entry")
	}
	UnregisterHostCtrlFrame(inline)

	const slots = 404
	wideBytes, err := HostCtrlFrameBytesForSlots(slots)
	if err != nil {
		t.Fatal(err)
	}
	wide := make([]byte, wideBytes)
	if err := InitHostCtrlFrame(wide); err != nil {
		t.Fatal(err)
	}
	if err := RegisterHostCtrlFrame(wide); err != nil {
		t.Fatal(err)
	}
	widePtr := slicePtr(wide)
	t.Cleanup(func() { UnregisterHostCtrlFrame(wide) })
	if got, loaded := registeredHostCtrlFrames.Load(widePtr); !loaded || got.(int) != wideBytes {
		t.Fatalf("wide registry entry = %v, %v; want %d", got, loaded, wideBytes)
	}
	if got := len(hostCtrlFrame(widePtr)); got != wideBytes {
		t.Fatalf("registered wide frame length = %d, want %d", got, wideBytes)
	}
	UnregisterHostCtrlFrame(wide)
	if _, loaded := registeredHostCtrlFrames.Load(widePtr); loaded {
		t.Fatal("wide control-frame registration survived cleanup")
	}
	if got := len(hostCtrlFrame(widePtr)); got != HostCtrlFrameBytes {
		t.Fatalf("unregistered frame length = %d, want inline fallback %d", got, HostCtrlFrameBytes)
	}
}
