//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestMunmapRangeReleasesAnonymousMapping(t *testing.T) {
	mem, err := mmapRW(pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if err := munmapRange(uintptr(unsafe.Pointer(&mem[0])), uintptr(len(mem))); err != nil {
		t.Fatalf("munmapRange: %v", err)
	}
}

func TestMadviseDontNeedMappedAndEmptyRanges(t *testing.T) {
	if err := madviseDontNeed(nil); err != nil {
		t.Fatalf("madviseDontNeed(nil): %v", err)
	}
	mem, err := mmapRW(pageSize)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(mem)
	mem[0] = 1
	if err := madviseDontNeed(mem); err != nil {
		t.Fatalf("madviseDontNeed(mapped): %v", err)
	}
}

func TestNormalizeMemorySizesBoundaries(t *testing.T) {
	for _, tc := range []struct {
		initial, max                      int
		wantInitial, wantMax, wantReserve int
	}{
		{0, 0, 0, 0, minClassicLinMemReserveBytes},
		{100, 10, 100, 100, minClassicLinMemReserveBytes},
		{100, 70000, 100, 70000, 70000},
		{100, maxClassicLinMemBytes + 1, 100, maxClassicLinMemBytes, maxClassicLinMemBytes},
		{maxClassicLinMemBytes + 1, maxClassicLinMemBytes, maxClassicLinMemBytes, maxClassicLinMemBytes, maxClassicLinMemBytes},
	} {
		initial, max, reserve := normalizeMemorySizes(tc.initial, tc.max)
		if initial != tc.wantInitial || max != tc.wantMax || reserve != tc.wantReserve {
			t.Fatalf("normalizeMemorySizes(%d, %d) = (%d, %d, %d), want (%d, %d, %d)", tc.initial, tc.max, initial, max, reserve, tc.wantInitial, tc.wantMax, tc.wantReserve)
		}
	}
}

func TestJobMemoryRestoreAndBasedataControl(t *testing.T) {
	j, err := NewJobMemoryGrowable(128, 65536)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	copy(j.CurrentBytes(), []byte("before"))
	j.putU32(offActualLinMemByteSize, 16)
	copy(j.CurrentBytes(), []byte("grown-tail"))
	j.RestoreLinear([]byte("after"))
	if got := string(j.CurrentBytes()); got != "after" {
		t.Fatalf("restored memory = %q", got)
	}
	if j.LinearMemory()[8] != 0 {
		t.Fatalf("grown tail = %#x, want zero", j.LinearMemory()[8])
	}
	if got := len(j.HostBytes()); got != len("after") {
		t.Fatalf("host bytes length = %d", got)
	}

	j.SetTablePtr(1)
	j.SetFuncRefDesc(2)
	j.SetPassiveElemPtr(3)
	j.SetTableDirPtr(4)
	if got := j.TableDirPtr(); got != 4 || j.getU64(offTablePtr) != 1 || j.getU64(offFuncRefDescPtr) != 2 || j.getU64(offPassiveElemPtr) != 3 {
		t.Fatal("basedata pointer fields did not round-trip")
	}
	snap := j.SnapshotBasedata()
	j.SetTableDirPtr(9)
	j.RestoreBasedata(snap)
	if j.TableDirPtr() != 4 {
		t.Fatal("basedata snapshot did not restore")
	}
	if base, length := j.ReserveRange(); base != 0 || length != 0 {
		t.Fatalf("classic reservation = %#x/%#x", base, length)
	}
}

func TestJobMemoryHasTrapCellDetectsCrossInstanceOverwrite(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	trap := make([]byte, 8)
	if jm.HasTrapCell(trap) {
		t.Fatal("fresh job memory unexpectedly has the trap cell bound")
	}
	if err := jm.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	if !jm.HasTrapCell(trap) {
		t.Fatal("bound trap cell was not recognized")
	}
	jm.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap))+8)
	if jm.HasTrapCell(trap) {
		t.Fatal("overwritten trap-cell pointer was not detected")
	}
}

func TestTrapAndSlotFormattingHelpers(t *testing.T) {
	if TrapDivZero.String() != "integer division by zero" || (&TrapError{Code: TrapDivZero}).Error() != "wasm trap: integer division by zero" {
		t.Fatal("known trap formatting mismatch")
	}
	if got := TrapCode(99).String(); got != "trap(99)" {
		t.Fatalf("unknown trap = %q", got)
	}
	for _, tc := range []struct {
		n    int
		want int
		err  bool
	}{{0, 8, false}, {3, 24, false}, {-1, 0, true}} {
		got, err := SlotBytes(tc.n)
		if got != tc.want || (err != nil) != tc.err {
			t.Fatalf("SlotBytes(%d) = %d, %v", tc.n, got, err)
		}
	}
}

func TestEngineAndArenaReuseMappings(t *testing.T) {
	eng, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	if eng.StackLimit() == 0 {
		t.Fatal("engine stack limit is zero")
	}
	if err := ReleaseEngine(eng); err != nil {
		t.Fatal(err)
	}
	reusedEng, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	if reusedEng != eng {
		t.Fatal("engine was not reused from the one-slot cache")
	}
	if err := reusedEng.Close(); err != nil {
		t.Fatal(err)
	}

	arena, err := AcquireArena(32)
	if err != nil {
		t.Fatal(err)
	}
	arena.AllocNoZero(8)[0] = 0xff
	if err := ReleaseArena(arena); err != nil {
		t.Fatal(err)
	}
	reusedArena, err := AcquireArena(32)
	if err != nil {
		t.Fatal(err)
	}
	if reusedArena != arena {
		t.Fatal("arena was not reused from the one-slot cache")
	}
	if got := reusedArena.Alloc(8)[0]; got != 0 {
		t.Fatalf("reused arena byte = %#x, want zero", got)
	}
	if err := reusedArena.Close(); err != nil {
		t.Fatal(err)
	}

	mem, entry, err := MapCode([]byte{0xc0, 0x03, 0x5f, 0xd6}) // arm64 ret
	if err != nil {
		t.Fatal(err)
	}
	if len(mem) < 4 || entry == 0 {
		t.Fatalf("mapped code = %d bytes at %#x", len(mem), entry)
	}
	if err := Unmap(mem); err != nil {
		t.Fatal(err)
	}
	odd, _, err := MapCode([]byte{0xc0, 0x03, 0x5f, 0xd6, 0})
	if err != nil || len(odd) != pageSize || odd[4] != 0 {
		t.Fatalf("odd-sized mapped code = %d bytes, %v", len(odd), err)
	}
	if err := Unmap(odd); err != nil {
		t.Fatal(err)
	}
	if err := Unmap(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeMappingBoundaryHelpers(t *testing.T) {
	if slicePtr(nil) != 0 || slicePtr([]byte{}) != 0 {
		t.Fatal("empty mapping slice has a non-zero pointer")
	}
	if roundUpPage(-1) != pageSize || roundUpPage(0) != pageSize || roundUpPage(pageSize+1) != 2*pageSize {
		t.Fatal("page rounding changed")
	}
	if err := ReleaseArena(nil); err != nil {
		t.Fatalf("ReleaseArena(nil): %v", err)
	}
	large, err := NewArena(InstantiateArenaSize + 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReleaseArena(large); err != nil {
		t.Fatalf("ReleaseArena(large): %v", err)
	}

	first, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReleaseJobMemory(first); err != nil {
		t.Fatalf("ReleaseJobMemory(first): %v", err)
	}
	second, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReleaseJobMemory(second); err != nil {
		t.Fatalf("ReleaseJobMemory(second): %v", err)
	}
	if err := ReleaseJobMemory(nil); err != nil {
		t.Fatalf("ReleaseJobMemory(nil): %v", err)
	}
	cached, err := AcquireJobMemoryGrowable(65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if cached != first {
		t.Fatal("expected cached job memory")
	}
	if err := cached.Close(); err != nil {
		t.Fatalf("Close cached job memory: %v", err)
	}
}

func TestInstantiateFootprintValidationEdges(t *testing.T) {
	for _, fp := range []InstantiateFootprint{
		{FuncImportCount: -1},
		{TableSize: 1},
		{HasTable: true, TableSize: 2, TableCapacity: 1},
		{HasTable: false, TableCapacities: []int{1}},
		{HasTable: true, TableCapacities: []int{1}, ImportedTableCount: 2},
		{HasTable: true, TableCapacities: []int{1}, TableEntryBytes: []int{7}},
	} {
		if _, err := InstantiateArenaNeed(fp); err == nil {
			t.Fatalf("invalid footprint accepted: %#v", fp)
		}
	}
	need, err := InstantiateArenaNeed(InstantiateFootprint{FuncImportCount: 1, GlobalCount: 2, HasTable: true, TableSize: 1, TableCapacity: 2, PassiveElemCount: 1, PassiveDataCount: 1, MaxParamSlots: 1, MaxResultSlots: 2})
	if err != nil || need <= 0 {
		t.Fatalf("valid footprint = %d, %v", need, err)
	}
}
