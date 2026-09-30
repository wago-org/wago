package wasm

import "testing"

func TestIndexedLocalLookupMatchesLinear(t *testing.T) {
	for _, count := range []int{0, 1, 2, 3, 8, 128} {
		v := funcValidator{localParams: []ValType{I32, F32}, localRuns: make([]LocalRun, count)}
		for i := range v.localRuns {
			v.localRuns[i] = LocalRun{Count: uint32(1 + i%3), Type: []ValType{I64, F64, V128}[i%3]}
		}
		v.indexLocalRuns()
		if count <= 2 && len(v.localRunEnds) != 0 {
			t.Fatal("tiny run list built an index")
		}
		for idx := uint32(0); idx < uint32(4+count*3); idx++ {
			got, gok := v.localType(idx)
			want, wok := LocalType(v.localParams, v.localRuns, idx)
			if got != want || gok != wok {
				t.Fatalf("runs=%d index=%d: indexed=%v/%v linear=%v/%v", count, idx, got, gok, want, wok)
			}
		}
		if _, ok := v.localType(^uint32(0)); ok {
			t.Fatal("wrapped local index accepted")
		}
	}
}
