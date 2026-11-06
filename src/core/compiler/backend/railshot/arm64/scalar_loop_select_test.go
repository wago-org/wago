//go:build arm64

package arm64

import "testing"

func TestScalarLoopSelectAndMaskRenameOnlyRegisterReads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		sf := uint32(0)
		if wide {
			sf = 1 << 31
		}
		csel := uint32(0x1a800000|19<<16|3<<12|19<<5|3) | sf
		if rd, ok := scalarLoopWord(csel); !ok || rd != X3 {
			t.Fatalf("CSEL rejected: wide=%v rd=%v ok=%v", wide, rd, ok)
		}
		want := (csel &^ uint32(31<<5|31<<16)) | 9<<5 | 9<<16
		if got := renameScalarLoopReads(csel, X19, X9); got != want {
			t.Fatalf("CSEL wide=%v got=%08x want=%08x", wide, got, want)
		}
		if got := renameScalarLoopReads(csel, X3, X9); got != csel {
			t.Fatal("CSEL condition or destination renamed as a read")
		}
		tst := uint32(0x7200001f|19<<16|19<<5) | sf
		if rd, ok := scalarLoopWord(tst); !ok || rd != regNone {
			t.Fatalf("TST rejected: wide=%v rd=%v ok=%v", wide, rd, ok)
		}
		want = (tst &^ uint32(31<<5)) | 9<<5
		if got := renameScalarLoopReads(tst, X19, X9); got != want {
			t.Fatalf("logical immediate rewritten: wide=%v got=%08x want=%08x", wide, got, want)
		}
		for _, word := range []uint32{0x1200001f | 19<<5 | sf, 0x3200001f | 19<<5 | sf, 0x12000000 | 31<<5 | 3 | sf, csel | 1<<10, (csel &^ uint32(15<<12)) | 14<<12} {
			if _, ok := scalarLoopWord(word); ok {
				t.Fatalf("unsupported encoding admitted: %08x", word)
			}
		}
	}
}
