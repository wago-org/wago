package shared

import (
	"reflect"
	"testing"
)

func TestSourceRangesFollowFinalCompaction(t *testing.T) {
	deletions := []DeletedRange{{Off: 4, Len: 4}, {Off: 12, Len: 4}}
	m, err := NewOffsetMap(20, deletions)
	if err != nil {
		t.Fatal(err)
	}
	source := []NativeSourceRange{
		{Offset: 0, Size: 6, Function: 3, WasmOffset: 1},
		{Offset: 6, Size: 2, Function: 3, WasmOffset: 2},
		{Offset: 8, Size: 6, Function: 3, WasmOffset: 3},
		{Offset: 14, Size: 4, Function: 3, WasmOffset: 4},
	}
	got, err := RemapNativeSources(source, &m)
	if err != nil {
		t.Fatal(err)
	}
	want := []NativeSourceRange{{Offset: 0, Size: 4, Function: 3, WasmOffset: 1}, {Offset: 4, Size: 4, Function: 3, WasmOffset: 3}, {Offset: 8, Size: 2, Function: 3, WasmOffset: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if _, ok := m.Map(6); ok {
		t.Fatal("range remapping weakened relocation mapping")
	}
	if source[0].Size != 6 {
		t.Fatal("source directory mutated")
	}
}

func TestSourceRangeMapsMatchSurvivingBytes(t *testing.T) {
	for length := 1; length <= 20; length++ {
		for start := 0; start < length; start++ {
			for end := start + 1; end <= length; end++ {
				deletions := []DeletedRange{{Off: uint32(start), Len: uint32(end - start)}}
				narrow, err := NewOffsetMap(length, deletions)
				if err != nil {
					t.Fatal(err)
				}
				wide, err := NewWideOffsetMap(length, deletions)
				if err != nil {
					t.Fatal(err)
				}
				for a := 0; a <= length; a++ {
					for b := a; b <= length; b++ {
						before, surviving := 0, 0
						for pc := 0; pc < b; pc++ {
							if pc >= start && pc < end {
								continue
							}
							if pc < a {
								before++
							} else {
								surviving++
							}
						}
						for _, m := range []sourceRangeMapper{&narrow, &wide} {
							lo, hi, ok := m.MapRange(a, b)
							if !ok || lo != before || hi-lo != surviving {
								t.Fatalf("len=%d delete=[%d,%d) source=[%d,%d): %d,%d,%v", length, start, end, a, b, lo, hi, ok)
							}
						}
					}
				}
			}
		}
	}
}

func TestSourceRangesPreserveUnknownGapsAndRejectInvalidInput(t *testing.T) {
	m, err := NewOffsetMap(16, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := []NativeSourceRange{{Offset: 0, Size: 4, Function: 2, WasmOffset: 3}, {Offset: 8, Size: 4, Function: 2, WasmOffset: 3}, {Offset: 12, Size: 4, Function: 2, WasmOffset: 3}}
	got, err := RemapNativeSources(source, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []NativeSourceRange{{Offset: 0, Size: 4, Function: 2, WasmOffset: 3}, {Offset: 8, Size: 8, Function: 2, WasmOffset: 3}}) {
		t.Fatal(got)
	}
	for _, bad := range [][]NativeSourceRange{{{Offset: 15, Size: 2, Function: 0, WasmOffset: 0}}, {{Offset: 0, Size: 0, Function: 0, WasmOffset: 0}}, {{Offset: 0, Size: 8, Function: 0, WasmOffset: 0}, {Offset: 4, Size: 2, Function: 0, WasmOffset: 0}}, {{Offset: ^uint64(0), Size: 2, Function: 0, WasmOffset: 0}}} {
		if _, err := RemapNativeSources(bad, &m); err == nil {
			t.Fatal("invalid source range accepted", bad)
		}
	}
}

func TestSourceRangesKeepDistinctInlineContexts(t *testing.T) {
	m, err := NewOffsetMap(12, []DeletedRange{{Off: 4, Len: 4}})
	if err != nil {
		t.Fatal(err)
	}
	ranges := []NativeSourceRange{{Offset: 0, Size: 4, Function: 3, WasmOffset: 5, InlineParent: 1}, {Offset: 8, Size: 4, Function: 3, WasmOffset: 5, InlineParent: 2}}
	mapped, err := RemapNativeSources(ranges, &m)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 2 || mapped[0].InlineParent != 1 || mapped[1].InlineParent != 2 {
		t.Fatal("compaction merged distinct inline sites", mapped)
	}
	combined := OverlayNativeSources(mapped, nil)
	if len(combined) != 2 {
		t.Fatal("overlay merged distinct inline sites", combined)
	}
}
