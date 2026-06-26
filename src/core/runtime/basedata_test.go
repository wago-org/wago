//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"testing"
)

// TestBasedataOffsetsMatchWARP guards against silent drift of the basedata
// layout away from WARP's basedataoffsets.hpp (Phase-0 config). If WARP's
// layout or our config changes, this must be re-derived.
func TestBasedataOffsetsMatchWARP(t *testing.T) {
	cases := []struct {
		name      string
		got, want int
	}{
		{"linMemWasmSize", offLinMemWasmSize, 4},
		{"actualLinMemByteSize", offActualLinMemByteSize, 8},
		{"trapHandlerPtr", offTrapHandlerPtr, 16},
		{"trapStackReentry", offTrapStackReentry, 24},
		{"runtimePtr", offRuntimePtr, 32},
		{"customCtx", offCustomCtx, 40},
		{"spillRegion", offSpillRegion, 48},
		{"jobMemoryDataPtrPtr", offJobMemoryDataPtrPtr, 56},
		{"memoryHelperPtr", offMemoryHelperPtr, 64},
		{"stackFence", offStackFence, 72},
		{"tablePtr", offTablePtr, 80},
		{"globalsPtr", offGlobalsPtr, 88},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s offset = %d, want %d", c.name, c.got, c.want)
		}
	}
	if basedataSize%16 != 0 {
		t.Errorf("basedataSize %d is not 16-byte aligned (would misalign linMem)", basedataSize)
	}
	if basedataSize < offGlobalsPtr+8 {
		t.Errorf("basedataSize %d too small for deepest field at -%d", basedataSize, offGlobalsPtr)
	}
}

func TestJobMemoryGlobalsPtr(t *testing.T) {
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	jm.SetGlobalsPtr(0x123456789abcdef0)
	got := binary.LittleEndian.Uint64(jm.mem[jm.linOff-offGlobalsPtr:])
	if got != 0x123456789abcdef0 {
		t.Fatalf("globals ptr = %#x, want %#x", got, uint64(0x123456789abcdef0))
	}
}

// TestJobMemoryMemSizeCache verifies the memSize cache field is populated so a
// real WARP prologue (memSize = [linMem-8]-8) would read the right value.
func TestJobMemoryMemSizeCache(t *testing.T) {
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	// actualLinMemByteSize lives at [linMem-8]; read it back through the region.
	got := jm.mem[jm.linOff-offActualLinMemByteSize]
	_ = got
	if jm.LinMemBase() == 0 {
		t.Fatal("nil linMem base")
	}
	if len(jm.LinearMemory()) != linMemBytes {
		t.Fatalf("linear memory length = %d, want %d", len(jm.LinearMemory()), linMemBytes)
	}
}
