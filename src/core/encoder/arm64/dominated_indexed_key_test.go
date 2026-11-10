package arm64

import "testing"

func TestDominatedIndexedKeyDiamond(t *testing.T) {
	for _, outside := range []bool{false, true} {
		var a Asm
		if outside {
			a.word(0x14000007)
		} // outside origin jumps directly to candidate
		a.AddShifted(X16, X26, X20, 0, false)
		a.word(0xB9400601) // LDR W1,[X16,#4]
		a.word(0x34000060) // CBZ W0, +12
		a.MovImm32(X1, 1)
		a.word(0x14000002) // B +8
		a.MovImm32(X1, 2)
		candidate := a.Len()
		a.AddShifted(X16, X26, X20, 0, false)
		a.word(0xB9400A02) // LDR W2,[X16,#8]
		original := a.Len()
		hits := a.FoldDominatedIndexedBasesByKey()
		want := 1
		if outside {
			want = 0
		}
		if hits != want || a.Len() != original {
			t.Fatalf("outside=%v hits=%d want=%d bytes=%d want=%d", outside, hits, want, a.Len(), original)
		}
		if hits == 1 && a.wordAt(candidate) != 0xD503201F {
			t.Fatal("candidate is not NOP")
		}
	}
}

func TestDominatedIndexedKeyProtectedWrites(t *testing.T) {
	for _, reg := range []Reg{X16, X26, X20} {
		for _, wide := range []bool{false, true} {
			var a Asm
			a.AddShifted(X16, X26, X20, 0, false)
			if wide {
				a.MovImm64(reg, 1)
			} else {
				a.MovImm32(reg, 1)
			}
			a.AddShifted(X16, X26, X20, 0, false)
			if hits := a.FoldDominatedIndexedBasesByKey(); hits != 0 {
				t.Fatalf("write reg=%d wide=%v hits=%d", reg, wide, hits)
			}
		}
	}
}

func TestDominatedIndexedKeyEntryAndBackedge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target int
		back   bool
		want   int
	}{
		{"seed entry", 4, false, 1}, {"middle entry", 8, false, 0}, {"candidate entry", 12, false, 0}, {"backedge middle", 0, true, 0},
	} {
		var a Asm
		if !tc.back {
			a.word(0x14000000 | uint32(tc.target/4))
		}
		a.AddShifted(X16, X26, X20, 0, false)
		a.word(0xB9400601)
		a.AddShifted(X16, X26, X20, 0, false)
		if tc.back {
			a.word(0x17FFFFFE)
		} // from12 to4, bypassing seed
		if hits := a.FoldDominatedIndexedBasesByKey(); hits != tc.want {
			t.Fatalf("%s hits=%d want=%d", tc.name, hits, tc.want)
		}
	}
}

func TestDominatedIndexedKeyUnknownAndCalls(t *testing.T) {
	for _, word := range []uint32{0x94000000, 0xD63F0000, 0xD61F0000, 0xD5033BBF, 0xA8C10601, 0xFFFFFFFF, 0x8B010210} {
		var a Asm
		a.AddShifted(X16, X26, X20, 0, false)
		a.word(word)
		a.AddShifted(X16, X26, X20, 0, false)
		if hits := a.FoldDominatedIndexedBasesByKey(); hits != 0 {
			t.Fatalf("unknown/call word=%08x hits=%d", word, hits)
		}
	}
}

func TestDominatedIndexedKeyLimits(t *testing.T) {
	for _, count := range []int{63, 64} {
		var a Asm
		a.AddShifted(X16, X26, X20, 0, false)
		for i := 0; i < count; i++ {
			a.Nop()
		}
		a.AddShifted(X16, X26, X20, 0, false)
		want := 1

		if hits := a.FoldDominatedIndexedBasesByKey(); hits != want {
			t.Fatalf("lookback count=%d hits=%d want=%d", count, hits, want)
		}
	}
	var edges Asm
	for i := 0; i < 129; i++ {
		edges.word(0x14000001)
	}
	edges.AddShifted(X16, X26, X20, 0, false)
	edges.Nop()
	edges.AddShifted(X16, X26, X20, 0, false)
	if hits := edges.FoldDominatedIndexedBasesByKey(); hits != 0 {
		t.Fatalf("edge overflow hits=%d", hits)
	}
	var large Asm
	large.AddShifted(X16, X26, X20, 0, false)
	large.Nop()
	large.AddShifted(X16, X26, X20, 0, false)
	for large.Len() <= 4096 {
		large.Nop()
	}
	if hits := large.FoldDominatedIndexedBasesByKey(); hits != 0 {
		t.Fatalf("function limit hits=%d", hits)
	}
}

func TestDominatedIndexedKeyWriteClassGoldens(t *testing.T) {
	// Independent AArch64 instruction words: vary only the architectural Rd.
	for _, tc := range []struct {
		name string
		word uint32
	}{
		{"logical register", 0x0A020023}, {"logical immediate", 0x12000C23},
		{"MOVZ W", 0x52800023}, {"MOVZ X", 0xD2800023}, {"UBFM", 0x53017C23},
		{"MADD", 0x1B020C23}, {"UDIV", 0x1AC20823}, {"RBIT", 0x5AC00023},
		{"CSEL", 0x1A820023}, {"signed load", 0xB9800423},
	} {
		for _, dst := range []Reg{X3, X16, X20, X26} {
			word := tc.word&^31 | uint32(dst)
			want := dst != X16 && dst != X20 && dst != X26
			if got := preservesDominatedIndexedBase(word, X26, X20); got != want {
				t.Fatalf("%s word=%08x got=%v want=%v", tc.name, word, got, want)
			}
		}
	}
	if !preservesDominatedIndexedBase(0x7A420020, X26, X20) {
		t.Fatal("CCMP only writes flags")
	}
}

// Equal address expressions from separate incoming definitions meet as one
// physical fact. A changed register pair must still fail the intersection.
func TestDominatedIndexedKeyIndependentSeedsAtJoin(t *testing.T) {
	for _, same := range []bool{false, true} {
		var a Asm
		a.word(0x34000080) // CBZ W0, +16
		a.AddShifted(X16, X26, X20, 0, false)
		a.Nop()
		a.word(0x14000003) // B +12 to join
		index := X21
		if same {
			index = X20
		}
		a.AddShifted(X16, X26, index, 0, false)
		a.Nop()
		join := a.Len()
		a.AddShifted(X16, X26, X20, 0, false)
		a.word(0xB9400201) // LDR W1,[X16]
		hits := a.FoldDominatedIndexedBasesByKey()
		want := 0
		if same {
			want = 1
		}
		if hits != want {
			t.Fatalf("same=%v hits=%d want=%d", same, hits, want)
		}
		if same && a.wordAt(join) != 0xD503201F {
			t.Fatal("join did not reuse equivalent seeds")
		}
	}
}
