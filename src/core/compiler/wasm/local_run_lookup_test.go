package wasm

import "testing"

func TestValidatorLocalLookupReuse(t *testing.T) {
	v := funcValidator{}
	params := []ValType{I64, F64}
	for _, n := range []int{0, 1, 8, 9, 64, 3, 32, 0} {
		runs := make([]LocalRun, n)
		for i := range runs {
			runs[i] = LocalRun{Count: uint32(i % 3), Type: []ValType{I32, I64, F32, F64}[i%4]}
		}
		v.localParams, v.localRuns = params, runs
		v.localCount, _ = LocalCount(params, runs)
		v.prepareLocalLookup()
		for index := uint32(0); uint64(index) < v.localCount+2; index++ {
			want, ok := LocalType(params, runs, index)
			got, gotOK := v.localTypeIndexed(index)
			if got != want || gotOK != ok {
				t.Fatalf("runs=%d index=%d got=%v/%t want=%v/%t", n, index, got, gotOK, want, ok)
			}
		}
	}
}

func TestValidatorLocalLookupWideRuns(t *testing.T) {
	// Wide counts remain compact; this test allocates only ten run records.
	runs := make([]LocalRun, 10)
	runs[0] = LocalRun{Count: ^uint32(0) - 10, Type: I64}
	runs[5] = LocalRun{Count: 10, Type: F32}
	v := funcValidator{localRuns: runs, localCount: uint64(^uint32(0))}
	v.prepareLocalLookup()
	for i := 0; i < 8; i++ {
		v.localTypeIndexed(^uint32(0) - 1)
	}
	if len(v.localRunStarts) != len(runs) {
		t.Fatal("wide boundary checks require the indexed path")
	}
	for _, index := range []uint32{0, ^uint32(0) - 12, ^uint32(0) - 10, ^uint32(0) - 1, ^uint32(0)} {
		want, ok := LocalType(nil, runs, index)
		got, gotOK := v.localTypeIndexed(index)
		if got != want || gotOK != ok {
			t.Fatalf("index=%d got=%v/%t want=%v/%t", index, got, gotOK, want, ok)
		}
	}
}

func TestValidatorLocalLookupBuildIsLazy(t *testing.T) {
	runs := make([]LocalRun, 64)
	for i := range runs {
		runs[i] = LocalRun{Count: 1, Type: I32}
	}
	v := funcValidator{localRuns: runs, localCount: 64}
	v.prepareLocalLookup()
	for i := 0; i < 4; i++ {
		if _, ok := v.localTypeIndexed(63); !ok {
			t.Fatal("last local not found")
		}
		if len(v.localRunStarts) != 0 {
			t.Fatal("index built before prior scan work justified it")
		}
	}
	if _, ok := v.localTypeIndexed(63); !ok || len(v.localRunStarts) != 64 {
		t.Fatal("index not built after repeated full scans")
	}
	v.prepareLocalLookup()
	for i := 0; i < 64; i++ {
		v.localTypeIndexed(0)
	}
	if len(v.localRunStarts) != 0 {
		t.Fatal("early-run lookups should not build an index")
	}
}
