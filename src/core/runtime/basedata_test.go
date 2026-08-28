//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/abi"
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
		{"actualLinMemByteSize64", offActualLinMemByteSize64, abi.ActualLinMemByteSize64Offset},
		{"trapHandlerPtr", offTrapHandlerPtr, 16},
		{"trapStackReentry", offTrapStackReentry, 24},
		{"runtimePtr", offRuntimePtr, 32},
		{"customCtx", offCustomCtx, 40},
		{"spillRegion", offSpillRegion, 48},
		{"jobMemoryDataPtrPtr", offJobMemoryDataPtrPtr, 56},
		{"memoryDirPtr", offMemoryDirPtr, abi.MemoryDirPtrOffset},
		{"stackFence", offStackFence, 72},
		{"tablePtr", offTablePtr, 80},
		{"funcRefDescPtr", offFuncRefDescPtr, abi.FuncRefDescPtrOffset},
		{"tableDirPtr", offTableDirPtr, abi.TableDirPtrOffset},
		{"passiveElemPtr", offPassiveElemPtr, abi.PassiveElemPtrOffset},
		{"globalsPtr", offGlobalsPtr, abi.GlobalsPtrOffset},
		{"passiveDataPtr", offPassiveDataPtr, abi.PassiveDataPtrOffset},
		{"importDispatchPtr", offImportDispatchPtr, abi.ImportDispatchPtrOffset},
		{"profileCountersPtr", offProfileCountersPtr, abi.ProfileCountersPtrOffset},
		{"tierEntriesPtr", offTierEntriesPtr, abi.TierEntriesPtrOffset},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s offset = %d, want %d", c.name, c.got, c.want)
		}
	}
	if basedataSize%16 != 0 {
		t.Errorf("basedataSize %d is not 16-byte aligned (would misalign linMem)", basedataSize)
	}
	if basedataSize < offTailArgs {
		t.Errorf("basedataSize %d too small for wrapper-tail scratch ending at -%d", basedataSize, offTailArgs)
	}
	if got := offTailArgs - (offImportDispatchPtr + 8); got != abi.TailArgsSlots*8 {
		t.Errorf("wrapper-tail scratch bytes = %d, want %d", got, abi.TailArgsSlots*8)
	}
}

func TestJobMemoryMetadataPointers(t *testing.T) {
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
	jm.SetPassiveDataPtr(0x0fedcba987654321)
	got = binary.LittleEndian.Uint64(jm.mem[jm.linOff-offPassiveDataPtr:])
	if got != 0x0fedcba987654321 {
		t.Fatalf("passive data ptr = %#x, want %#x", got, uint64(0x0fedcba987654321))
	}
	jm.SetProfileCountersPtr(0x1020304050607080)
	if got := jm.ProfileCountersPtr(); got != 0x1020304050607080 {
		t.Fatalf("profile counters ptr = %#x", got)
	}
	jm.SetTierEntriesPtr(0x8877665544332211)
	if got := jm.TierEntriesPtr(); got != 0x8877665544332211 {
		t.Fatalf("tier entries ptr = %#x", got)
	}
}

// TestJobMemoryMemSizeCache verifies both the compatibility u32 cache and the
// authoritative u64 cache used by native bounds checks.
func TestJobMemoryMemSizeCache(t *testing.T) {
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	if got := jm.getU32(offActualLinMemByteSize); got != linMemBytes {
		t.Fatalf("legacy byte-size cache = %d, want %d", got, linMemBytes)
	}
	if got := jm.getU64(offActualLinMemByteSize64); got != linMemBytes {
		t.Fatalf("u64 byte-size cache = %d, want %d", got, linMemBytes)
	}
	if jm.LinMemBase() == 0 {
		t.Fatal("nil linMem base")
	}
	if len(jm.LinearMemory()) != linMemBytes {
		t.Fatalf("linear memory length = %d, want %d", len(jm.LinearMemory()), linMemBytes)
	}
}

func TestAcquireJobMemoryGrowableReusesZeroedMemory(t *testing.T) {
	jm, err := AcquireJobMemoryGrowable(linMemBytes, linMemBytes)
	if err != nil {
		t.Fatal(err)
	}
	base := jm.LinMemBase()
	lin := jm.LinearMemory()
	for i := range lin[:1024] {
		lin[i] = 0xa5
	}
	jm.SetCustomCtx(0x1234)
	if err := ReleaseJobMemory(jm); err != nil {
		t.Fatal(err)
	}

	jm2, err := AcquireJobMemoryGrowable(linMemBytes, linMemBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseJobMemory(jm2)
	if got := jm2.LinMemBase(); got != base {
		t.Fatalf("LinMemBase = %#x, want cached base %#x", got, base)
	}
	lin2 := jm2.LinearMemory()
	for i, b := range lin2[:1024] {
		if b != 0 {
			t.Fatalf("reused linear memory byte %d = %#x, want 0", i, b)
		}
	}
	if got := binary.LittleEndian.Uint64(jm2.mem[jm2.linOff-offCustomCtx:]); got != 0 {
		t.Fatalf("custom ctx after reset = %#x, want 0", got)
	}
}

// TestAcquireJobMemoryGrowableReusesLargeReservation exercises the MADV_DONTNEED
// reclaim path (used region above jobMemoryReclaimThreshold): a large, dirtied
// reservation must read back fully zeroed on reuse, and the mapping must be
// reused (same base), not freshly mmap'd.
func TestAcquireJobMemoryGrowableReusesLargeReservation(t *testing.T) {
	const initial = jobMemoryReclaimThreshold + 512<<10 // forces the madvise path
	jm, err := AcquireJobMemoryGrowable(initial, initial)
	if err != nil {
		t.Fatal(err)
	}
	base := jm.LinMemBase()
	lin := jm.LinearMemory()
	for i := range lin {
		lin[i] = 0xa5
	}
	if err := ReleaseJobMemory(jm); err != nil {
		t.Fatal(err)
	}

	jm2, err := AcquireJobMemoryGrowable(initial, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseJobMemory(jm2)
	if got := jm2.LinMemBase(); got != base {
		t.Fatalf("LinMemBase = %#x, want reused base %#x", got, base)
	}
	for i, b := range jm2.LinearMemory() {
		if b != 0 {
			t.Fatalf("reused linear memory byte %d = %#x, want 0", i, b)
		}
	}
}

// TestAcquireJobMemoryGrowableReuseZeroesGrownPages verifies the reclaim covers
// pages faulted in by memory.grow, not just the initial region: an instance that
// grows and dirties a page beyond its initial size must not leak that data to a
// later, smaller-initial reuse of the same reservation.
func TestAcquireJobMemoryGrowableReuseZeroesGrownPages(t *testing.T) {
	const maxBytes = 4 << 20
	jm, err := AcquireJobMemoryGrowable(linMemBytes, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate memory.grow: native code raises the size cache in place, then the
	// guest writes into the newly in-bounds region. Dirty a page well past the
	// initial size to catch a reclaim that only zeroes [0,initial).
	grownBytes := maxBytes
	jm.putU32(offActualLinMemByteSize, uint32(grownBytes))
	jm.putU64(offActualLinMemByteSize64, uint64(grownBytes))
	full := jm.mem[jm.linOff : jm.linOff+grownBytes]
	full[grownBytes-1] = 0xff
	full[linMemBytes+8] = 0xff
	if err := ReleaseJobMemory(jm); err != nil {
		t.Fatal(err)
	}

	// Reacquire with the original small initial; the grown region must read zero.
	jm2, err := AcquireJobMemoryGrowable(linMemBytes, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseJobMemory(jm2)
	view := jm2.mem[jm2.linOff : jm2.linOff+grownBytes]
	for i, b := range view {
		if b != 0 {
			t.Fatalf("reused grown page byte %d = %#x, want 0", i, b)
		}
	}
}
