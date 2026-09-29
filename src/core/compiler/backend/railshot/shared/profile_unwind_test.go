package shared

import (
	"github.com/wago-org/wago/internal/jitprofile"
	"testing"
)

func TestUnwindTransitionsFollowCompaction(t *testing.T) {
	// SUB rsp, imm32 shrinks from seven to four bytes; ADD does likewise.
	m, err := NewOffsetMap(20, []DeletedRange{{Off: 4, Len: 3}, {Off: 16, Len: 3}})
	if err != nil {
		t.Fatal(err)
	}
	ranges := []jitprofile.UnwindRange{
		{Offset: 0, Size: 7, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
		{Offset: 7, Size: 12, CFARegister: 7, CFAOffset: 40, ReturnOffset: -8},
		{Offset: 19, Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
	}
	got, err := RemapNativeUnwind(ranges, &m)
	if err != nil {
		t.Fatal(err)
	}
	for pc := uint64(0); pc < 14; pc++ {
		r, ok := jitprofile.LookupUnwind(got, pc)
		want := int64(40)
		if pc < 4 || pc == 13 {
			want = 8
		}
		if !ok || r.CFAOffset != want {
			t.Fatal(pc, r)
		}
	}
	// Eliding a zero frame removes both adjustments; all surviving rules agree.
	for i := range ranges {
		ranges[i].CFAOffset = 8
	}
	elide, _ := NewOffsetMap(20, []DeletedRange{{Off: 0, Len: 7}, {Off: 12, Len: 7}})
	got, err = RemapNativeUnwind(ranges, &elide)
	if err != nil || len(got) != 1 || got[0].Offset != 0 || got[0].Size != 6 || got[0].CFAOffset != 8 {
		t.Fatal(got, err)
	}
	if ranges[0].Size != 7 {
		t.Fatal("mutated source directory")
	}
}

func TestUnwindRemapKeepsUnknownGaps(t *testing.T) {
	m, _ := NewOffsetMap(16, nil)
	ranges := []jitprofile.UnwindRange{{Size: 4, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8}, {Offset: 8, Size: 8, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8}}
	got, err := RemapNativeUnwind(ranges, &m)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if _, ok := jitprofile.LookupUnwind(got, 6); ok {
		t.Fatal("filled unknown gap")
	}
	ranges[0].Size = 9
	if _, err := RemapNativeUnwind(ranges, &m); err == nil {
		t.Fatal("accepted overlap")
	}
}
