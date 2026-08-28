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
		t.Fatalf("Engine size = %d, prior inline-only layout = %d", got, want)
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

func TestInstantiateArenaNeedAccountsDynamicFuncrefTypeIDs(t *testing.T) {
	base, err := InstantiateArenaNeed(InstantiateFootprint{})
	if err != nil {
		t.Fatal(err)
	}
	withIDs, err := InstantiateArenaNeed(InstantiateFootprint{FuncRefTypeIDCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := withIDs-base, 16; got != want {
		t.Fatalf("three compact funcref type IDs add %d bytes, want aligned %d", got, want)
	}
	if _, err := InstantiateArenaNeed(InstantiateFootprint{FuncRefTypeIDCount: -1}); err == nil {
		t.Fatal("negative funcref type-ID count unexpectedly accepted")
	}
}

func TestInstantiateArenaNeedAccountsV128GlobalCells(t *testing.T) {
	scalar, err := InstantiateArenaNeed(InstantiateFootprint{GlobalCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	withV128, err := InstantiateArenaNeed(InstantiateFootprint{GlobalCount: 2, V128GlobalCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := withV128-scalar, 8; got != want {
		t.Fatalf("one v128 global footprint delta = %d, want %d", got, want)
	}
	if _, err := InstantiateArenaNeed(InstantiateFootprint{GlobalCount: 1, V128GlobalCount: 2}); err == nil || !strings.Contains(err.Error(), "exceeds global count") {
		t.Fatalf("excess v128 global count error = %v", err)
	}
}

func TestInstantiateArenaNeedZeroLengthTableDescriptor(t *testing.T) {
	base := InstantiateFootprint{}
	withoutTable, err := InstantiateArenaNeed(base)
	if err != nil {
		t.Fatalf("InstantiateArenaNeed without table: %v", err)
	}
	withZeroTable, err := InstantiateArenaNeed(InstantiateFootprint{HasTable: true})
	if err != nil {
		t.Fatalf("InstantiateArenaNeed with zero-length table: %v", err)
	}
	if got, want := withZeroTable-withoutTable, 8; got != want {
		t.Fatalf("zero-length table footprint delta = %d, want descriptor header %d", got, want)
	}
	withPassiveData, err := InstantiateArenaNeed(InstantiateFootprint{PassiveDataCount: 2})
	if err != nil {
		t.Fatalf("InstantiateArenaNeed with passive data: %v", err)
	}
	if got, want := withPassiveData-withoutTable, 2*PassiveDataDescBytes; got != want {
		t.Fatalf("passive data footprint delta = %d, want %d", got, want)
	}
}

func TestInstantiateArenaNeedExcludesImportedTableDescriptors(t *testing.T) {
	oneImported, err := InstantiateArenaNeed(InstantiateFootprint{
		HasTable:           true,
		TableCapacities:    []int{0},
		ImportedTableCount: 1,
	})
	if err != nil {
		t.Fatalf("one imported table footprint: %v", err)
	}
	twoImported, err := InstantiateArenaNeed(InstantiateFootprint{
		HasTable:           true,
		TableCapacities:    []int{0, 0},
		ImportedTableCount: 2,
	})
	if err != nil {
		t.Fatalf("two imported tables footprint: %v", err)
	}
	if got, want := twoImported-oneImported, 16; got != want {
		t.Fatalf("second imported table footprint delta = %d, want 16-byte directory", got)
	}
	withLocal, err := InstantiateArenaNeed(InstantiateFootprint{
		HasTable:           true,
		TableCapacities:    []int{0, 0, 1},
		ImportedTableCount: 2,
	})
	if err != nil {
		t.Fatalf("two imported plus local footprint: %v", err)
	}
	if got, want := withLocal-twoImported, 48; got != want {
		t.Fatalf("local table after two imports footprint delta = %d, want 40-byte descriptor plus 8-byte directory growth", got)
	}
}

func TestFuncRefDescriptorLayout(t *testing.T) {
	if TableEntryBytes != 32 || FuncRefContextOffset != 32 || FuncRefDescBytes != 40 {
		t.Fatalf("funcref descriptor layout = table %d context %d bytes %d", TableEntryBytes, FuncRefContextOffset, FuncRefDescBytes)
	}
}

func TestInstantiateArenaNeedAllowsElementDropStateWithoutTable(t *testing.T) {
	baseline, err := InstantiateArenaNeed(InstantiateFootprint{})
	if err != nil {
		t.Fatalf("baseline footprint: %v", err)
	}
	withElements, err := InstantiateArenaNeed(InstantiateFootprint{ElemCount: 1, PassiveElemCount: 2, PassiveElemBytes: 24})
	if err != nil {
		t.Fatalf("element-only footprint: %v", err)
	}
	if got, want := withElements-baseline, 2*PassiveElemDescBytes+24; got != want {
		t.Fatalf("element-only footprint delta = %d, want %d", got, want)
	}
}

func TestInstantiateArenaNeedRejectsImpossibleTableShape(t *testing.T) {
	tests := []struct {
		name string
		fp   InstantiateFootprint
		want string
	}{
		{name: "table size without table", fp: InstantiateFootprint{TableSize: 1}, want: "without table"},
		{name: "too many imported tables", fp: InstantiateFootprint{HasTable: true, TableCapacities: []int{0}, ImportedTableCount: 2}, want: "exceeds table count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := InstantiateArenaNeed(tt.fp)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("InstantiateArenaNeed error = %v, want %q", err, tt.want)
			}
		})
	}
}
