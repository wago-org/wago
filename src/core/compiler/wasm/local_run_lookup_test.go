package wasm

import (
	"fmt"
	"testing"
)

func prepareLocalLookupForTest(t *testing.T, v *funcValidator, params []ValType, runs []LocalRun) {
	t.Helper()
	v.localParams, v.localRuns = params, runs
	var overflow bool
	v.localCount, overflow = LocalCount(params, runs)
	if overflow {
		t.Fatal("test local count overflow")
	}
	v.prepareLocalLookup()
}

func checkLocalLookupForTest(t *testing.T, v *funcValidator, idx uint32) {
	t.Helper()
	want, wantOK := LocalType(v.localParams, v.localRuns, idx)
	got, gotOK := v.localType(idx)
	if got != want || gotOK != wantOK {
		t.Fatalf("index=%d: lookup=%v/%t, linear=%v/%t", idx, got, gotOK, want, wantOK)
	}
}

func buildLocalLookupForTest(t *testing.T, v *funcValidator, idx uint32) {
	t.Helper()
	// Allow tuning the amortization threshold without pinning its exact value.
	for i := 0; i < 64 && len(v.localRunEnds) == 0; i++ {
		checkLocalLookupForTest(t, v, idx)
	}
	if len(v.localRunEnds) != len(v.localRuns) {
		t.Fatalf("repeated late lookups did not build index: got %d ends for %d runs", len(v.localRunEnds), len(v.localRuns))
	}
}

func TestValidatorLocalLookupMatchesLinear(t *testing.T) {
	for _, params := range [][]ValType{nil, {I64, F64}} {
		for _, n := range []int{0, 1, 2, 3, 8, 9, 64} {
			t.Run(fmt.Sprintf("params=%d/runs=%d", len(params), n), func(t *testing.T) {
				runs := make([]LocalRun, n)
				for i := range runs {
					runs[i] = LocalRun{Count: uint32(i % 3), Type: []ValType{I32, I64, F32, F64}[i%4]}
				}
				v := funcValidator{}
				prepareLocalLookupForTest(t, &v, params, runs)
				for idx := uint32(0); uint64(idx) < v.localCount+2; idx++ {
					// Check each index while cold, including repeated zero-count ends.
					v.prepareLocalLookup()
					checkLocalLookupForTest(t, &v, idx)
				}
				if n > 2 {
					buildLocalLookupForTest(t, &v, uint32(v.localCount-1))
				}
				for idx := uint32(0); uint64(idx) < v.localCount+2; idx++ {
					checkLocalLookupForTest(t, &v, idx)
				}
				checkLocalLookupForTest(t, &v, ^uint32(0))
			})
		}
	}
}

func TestValidatorLocalLookupWideRuns(t *testing.T) {
	// Large logical counts remain compact; no local-value array is allocated.
	runs := []LocalRun{
		{Type: F64},
		{Count: ^uint32(0) - 10, Type: I64},
		{Type: I32},
		{Count: 5, Type: F32},
		{Type: F64},
		{Count: 5, Type: I32},
		{Type: F32},
	}
	for _, params := range [][]ValType{nil, {F64, F32}} {
		t.Run(fmt.Sprintf("params=%d", len(params)), func(t *testing.T) {
			v := funcValidator{}
			prepareLocalLookupForTest(t, &v, params, runs)
			indexes := []uint32{0, 1, 2, ^uint32(0)}
			end := uint64(len(params))
			for _, run := range runs {
				end += uint64(run.Count)
				for _, boundary := range []uint64{end - 1, end, end + 1} {
					if boundary <= uint64(^uint32(0)) {
						indexes = append(indexes, uint32(boundary))
					}
				}
			}
			for _, idx := range indexes {
				v.prepareLocalLookup()
				checkLocalLookupForTest(t, &v, idx)
			}
			buildLocalLookupForTest(t, &v, ^uint32(0)-1)
			if end := v.localRunEnds[len(runs)-1]; end != v.localCount {
				t.Fatalf("last end=%d, want wide count %d", end, v.localCount)
			}
			for _, idx := range indexes {
				checkLocalLookupForTest(t, &v, idx)
			}
		})
	}
}

func TestValidatorLocalLookupBuildIsLazy(t *testing.T) {
	runs := make([]LocalRun, 64)
	for i := range runs {
		runs[i] = LocalRun{Count: 1, Type: []ValType{I32, I64, F32, F64}[i%4]}
	}
	v := funcValidator{}
	prepareLocalLookupForTest(t, &v, []ValType{F64, I64}, runs)
	if len(v.localRunEnds) != 0 || v.localLookupWork != 0 {
		t.Fatal("function preparation eagerly prepared lookup state")
	}
	for i := 0; i < 256; i++ {
		for _, idx := range []uint32{0, 1, 2, 3, uint32(v.localCount), ^uint32(0)} {
			checkLocalLookupForTest(t, &v, idx)
		}
	}
	if len(v.localRunEnds) != 0 || v.localLookupWork != 0 {
		t.Fatal("parameters, cheap-prefix reads, or invalid indexes accumulated scan work")
	}
	checkLocalLookupForTest(t, &v, uint32(v.localCount-1))
	if len(v.localRunEnds) != 0 {
		t.Fatal("a single late lookup built an index")
	}
	if v.localLookupWork == 0 {
		t.Fatal("late lookup did not record scan work")
	}
	buildLocalLookupForTest(t, &v, uint32(v.localCount-1))
	v.prepareLocalLookup()
	if len(v.localRunEnds) != 0 || v.localLookupWork != 0 {
		t.Fatal("new function retained an active index or scan work")
	}
	checkLocalLookupForTest(t, &v, uint32(v.localCount-1))
	if len(v.localRunEnds) != 0 {
		t.Fatal("previous function's scan work triggered eager rebuilding")
	}
}

func TestValidatorLocalLookupPendingBuildSkipsCheapQueries(t *testing.T) {
	runs := make([]LocalRun, 64)
	for i := range runs {
		runs[i] = LocalRun{Count: 1, Type: I32}
	}
	v := funcValidator{}
	prepareLocalLookupForTest(t, &v, []ValType{I64, F64}, runs)
	// Ensure the next costly lookup can build without pinning the scan budget.
	v.localLookupWork = ^uint64(0)
	for _, idx := range []uint32{0, 1, 2, 3, uint32(v.localCount), ^uint32(0)} {
		checkLocalLookupForTest(t, &v, idx)
		if len(v.localRunEnds) != 0 || v.localLookupWork != ^uint64(0) {
			t.Fatalf("cheap query %d consumed work or triggered a pending build", idx)
		}
	}
	checkLocalLookupForTest(t, &v, uint32(v.localCount-1))
	if len(v.localRunEnds) != len(runs) {
		t.Fatal("late query did not trigger the pending index build")
	}
}

func TestValidatorLocalLookupColdPathsDoNotAllocate(t *testing.T) {
	runs := make([]LocalRun, 64)
	for i := range runs {
		runs[i] = LocalRun{Count: 1, Type: I32}
	}
	for _, tc := range []struct {
		name    string
		runs    []LocalRun
		indexes []uint32
	}{
		{name: "unused", runs: runs},
		{name: "one-late-read", runs: runs, indexes: []uint32{65}},
		{name: "parameters", runs: runs, indexes: []uint32{0, 1}},
		{name: "cheap-prefix", runs: runs, indexes: []uint32{2, 3}},
		{name: "out-of-range", runs: runs, indexes: []uint32{66, ^uint32(0)}},
		{name: "two-runs", runs: runs[:2], indexes: []uint32{2, 3}},
		{name: "no-locals", indexes: []uint32{0, 1, 2, ^uint32(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := []ValType{I64, F64}
			count, _ := LocalCount(params, tc.runs)
			allocs := testing.AllocsPerRun(100, func() {
				v := funcValidator{localParams: params, localRuns: tc.runs, localCount: count}
				v.prepareLocalLookup()
				for _, idx := range tc.indexes {
					v.localType(idx)
				}
				if len(v.localRunEnds) != 0 {
					t.Fatal("cold path built an index")
				}
			})
			if allocs != 0 {
				t.Fatalf("cold local lookup allocated %g times, want zero", allocs)
			}
		})
	}
}

func TestValidatorLocalLookupZeroCountRuns(t *testing.T) {
	v := funcValidator{}
	runs := make([]LocalRun, 64)
	for i := range runs {
		runs[i].Type = []ValType{I32, I64, F32, F64}[i%4]
	}
	prepareLocalLookupForTest(t, &v, []ValType{I64, F64}, runs)
	for i := 0; i < 256; i++ {
		for _, idx := range []uint32{0, 1, 2, ^uint32(0)} {
			checkLocalLookupForTest(t, &v, idx)
		}
	}
	if len(v.localRunEnds) != 0 || v.localLookupWork != 0 {
		t.Fatal("empty local index space accumulated scan work")
	}
}

func TestValidatorLocalLookupBuildAllocations(t *testing.T) {
	runs := make([]LocalRun, 1024)
	for i := range runs {
		runs[i] = LocalRun{Count: 1, Type: I32}
	}
	allocs := testing.AllocsPerRun(100, func() {
		v := funcValidator{localRuns: runs, localCount: uint64(len(runs))}
		for i := 0; i < 64; i++ {
			v.localType(uint32(len(runs) - 1))
		}
		if len(v.localRunEnds) != len(runs) || cap(v.localRunEnds) != len(runs) {
			t.Fatal("index was not built with exactly the required capacity")
		}
	})
	if allocs != 1 {
		t.Fatalf("building one local index allocated %g times, want one", allocs)
	}
}

func TestValidatorLocalLookupReuse(t *testing.T) {
	v := funcValidator{}
	for iteration, n := range []int{2048, 2048, 2, 64, 0, 512, 3, 2048, 1} {
		params := []ValType{I64, F64}[:iteration%3]
		runs := make([]LocalRun, n)
		for i := range runs {
			runs[i] = LocalRun{Count: uint32(1 + (i+iteration)%3), Type: []ValType{I32, I64, F32, F64}[(i+iteration)%4]}
		}
		var backing *uint64
		previousCap := cap(v.localRunEnds)
		if previousCap != 0 {
			backing = &v.localRunEnds[:cap(v.localRunEnds)][0]
		}
		prepareLocalLookupForTest(t, &v, params, runs)
		if len(v.localRunEnds) != 0 || v.localLookupWork != 0 {
			t.Fatalf("iteration %d retained stale lookup state", iteration)
		}
		if previousCap > smallLocalRunIndexCapacity && n < previousCap/4 && cap(v.localRunEnds) != 0 {
			t.Fatalf("iteration %d retained oversized backing capacity %d for %d runs", iteration, cap(v.localRunEnds), n)
		}
		if n > 2 {
			buildLocalLookupForTest(t, &v, uint32(v.localCount-1))
			if iteration == 1 && &v.localRunEnds[0] != backing {
				t.Fatal("comparable large function did not reuse index storage")
			}
		}
		for idx := uint32(0); uint64(idx) < v.localCount+2; idx++ {
			checkLocalLookupForTest(t, &v, idx)
		}
		checkLocalLookupForTest(t, &v, ^uint32(0))
	}
}

func TestValidatorLocalLookupRetention(t *testing.T) {
	for _, tc := range []struct {
		name      string
		capacity  int
		nextRuns  int
		wantReuse bool
	}{
		{name: "at-floor", capacity: smallLocalRunIndexCapacity, wantReuse: true},
		{name: "large-to-empty", capacity: 2 * smallLocalRunIndexCapacity},
		{name: "large-to-small", capacity: 2 * smallLocalRunIndexCapacity, nextRuns: 2},
		{name: "comparable-large", capacity: 2 * smallLocalRunIndexCapacity, nextRuns: smallLocalRunIndexCapacity, wantReuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ends := make([]uint64, 3, tc.capacity)
			v := funcValidator{localRunEnds: ends, localLookupWork: 123}
			prepareLocalLookupForTest(t, &v, nil, make([]LocalRun, tc.nextRuns))
			if len(v.localRunEnds) != 0 || v.localLookupWork != 0 {
				t.Fatal("preparation retained active lookup state")
			}
			if tc.wantReuse {
				if cap(v.localRunEnds) != tc.capacity || &v.localRunEnds[:1][0] != &ends[0] {
					t.Fatal("reusable index storage was discarded")
				}
			} else if cap(v.localRunEnds) != 0 {
				t.Fatal("oversized index storage was retained after shrinking")
			}
		})
	}
}
