package wago

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestGCModuleFrameRootPlanAllowsMultipleNativePathsPerCall(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate:  true,
		Exact:      true,
		FrameBytes: 64,
		Locals:     []shared.GCFrameLocal{{Index: 0, Offset: 16}},
	}
	if !plan.SetLiveMasks([]uint64{1, 1}, 1, 1) {
		t.Fatal("failed to set live masks")
	}
	for _, site := range [][2]uint32{{4, 0}, {8, 0}, {12, 64}} {
		if !plan.AppendCallsite(site[0], site[1], []uint32{16}) {
			t.Fatal("failed to append callsite")
		}
	}
	if !plan.AppendSafepoint([]uint32{16}) {
		t.Fatal("failed to append safepoint")
	}
	if !validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan)) {
		t.Fatal("one logical call with three native return paths was rejected")
	}
}

func TestGCModuleFrameRootPlanRejectsPendingFunction(t *testing.T) {
	module := shared.NewGCModuleFrameRootPlan(1)
	if !module.MarkFunction(0) {
		t.Fatal("failed to mark collecting function")
	}
	if validGCModuleFrameRootPlan(module) {
		t.Fatal("module with an unpopulated collecting function was accepted")
	}
}

func TestValidGCFrameLocalsRequiresStrictIndexOrder(t *testing.T) {
	for _, locals := range [][]shared.GCFrameLocal{
		{{Index: 2, Offset: 16}, {Index: 2, Offset: 24}},
		{{Index: 3, Offset: 16}, {Index: 1, Offset: 24}},
	} {
		if validGCFrameLocals(locals, 64) {
			t.Fatalf("validGCFrameLocals accepted unsorted indexes: %#v", locals)
		}
	}
}

func TestGCModuleFrameRootPlanDerivesDenseSafepointIDs(t *testing.T) {
	plan := func(base uint32) *shared.GCFrameRootPlan {
		plan := &shared.GCFrameRootPlan{
			Candidate:     true,
			Exact:         true,
			FrameBytes:    8,
			SafepointBase: base,
		}
		if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
			t.Fatal("failed to set live masks")
		}
		if !plan.AppendSafepoint(nil) {
			t.Fatal("failed to append safepoint")
		}
		return plan
	}
	if !validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan(shared.GCSafepointIDMax-1))) {
		t.Fatal("maximum derived safepoint ID was rejected")
	}
	if validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan(shared.GCSafepointIDMax))) {
		t.Fatal("derived safepoint ID above the dispatch domain was accepted")
	}
	if validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan(0), plan(0))) {
		t.Fatal("overlapping derived safepoint ID ranges were accepted")
	}
}

func gcModuleFrameRootPlanForTest(t testing.TB, plans ...*shared.GCFrameRootPlan) *shared.GCModuleFrameRootPlan {
	t.Helper()
	module := shared.NewGCModuleFrameRootPlan(len(plans))
	count := 0
	for function, plan := range plans {
		if plan != nil {
			if !module.MarkFunction(function) {
				t.Fatalf("mark function root plan %d", function)
			}
			count++
		}
	}
	if !module.ReserveFunctions(count) {
		t.Fatalf("reserve %d function root plans", count)
	}
	for function, plan := range plans {
		if plan != nil {
			dst, ok := module.BeginFunction(function)
			if !ok {
				t.Fatalf("begin function root plan %d", function)
			}
			*dst = *plan
		}
	}
	return module
}

func TestNormalizeAdapterReturnOffsets(t *testing.T) {
	got := normalizeAdapterReturnOffsets([]uint32{300, 100, 300, 200, 100})
	want := []uint32{100, 200, 300}
	if !slices.Equal(got, want) {
		t.Fatalf("normalized adapter return offsets = %v, want %v", got, want)
	}
}

func TestCompiledGCFrameRootsSafepointByID(t *testing.T) {
	plan := &compiledGCFrameRoots{safepoints: []compiledGCFrameSafepoint{
		{id: 1, frameBytes: 32},
		{id: 2, frameBytes: 40},
		{id: 4, frameBytes: 48},
	}}
	for _, tc := range []struct {
		id   uint32
		want uint32
		ok   bool
	}{
		{id: 0},
		{id: 1, want: 32, ok: true},
		{id: 2, want: 40, ok: true},
		{id: 3},
		{id: 4, want: 48, ok: true},
		{id: 5},
	} {
		got := plan.safepointByID(tc.id)
		if !tc.ok {
			if got != nil {
				t.Fatalf("safepointByID(%d) = %+v, want nil", tc.id, got)
			}
			continue
		}
		if got == nil || got.id != tc.id || got.frameBytes != tc.want {
			t.Fatalf("safepointByID(%d) = %+v, want id=%d frameBytes=%d", tc.id, got, tc.id, tc.want)
		}
	}
}

func BenchmarkCompiledGCFrameRootsSafepointByIDDense(b *testing.B) {
	const count = 4096
	plan := &compiledGCFrameRoots{safepoints: make([]compiledGCFrameSafepoint, count)}
	for i := range plan.safepoints {
		plan.safepoints[i].id = uint32(i + 1)
	}
	b.ReportAllocs()
	var got *compiledGCFrameSafepoint
	for i := 0; i < b.N; i++ {
		got = plan.safepointByID(uint32(i%count + 1))
	}
	if got == nil {
		b.Fatal("dense lookup returned nil")
	}
}

func TestCompiledGCFrameRootsCallsiteIndex(t *testing.T) {
	for _, n := range []int{0, 1, 8, 9, 17} {
		plan := &compiledGCFrameRoots{callsites: make([]compiledGCFrameCallsite, n)}
		for i := range plan.callsites {
			plan.callsites[i].returnOffset = uint32(2*i + 2)
		}
		for offset := uint32(0); offset <= uint32(2*n+2); offset++ {
			want := -1
			if offset >= 2 && offset%2 == 0 && int(offset/2) <= n {
				want = int(offset/2) - 1
			}
			if got := plan.callsiteIndexByReturnOffset(offset); got != want {
				t.Fatalf("size=%d offset=%d index=%d, want%d", n, offset, got, want)
			}
		}
	}
	var empty *compiledGCFrameRoots
	if got := empty.callsiteIndexByReturnOffset(42); got != -1 {
		t.Fatalf("nil index=%d", got)
	}
}

var gcCallsiteBenchmarkSink int

func BenchmarkGCFrameCallsiteLookup(b *testing.B) {
	for _, n := range []int{8, 16, 100671} {
		plan := &compiledGCFrameRoots{callsites: make([]compiledGCFrameCallsite, n)}
		for i := range plan.callsites {
			plan.callsites[i].returnOffset = uint32(2*i + 2)
		}
		for _, miss := range []bool{false, true} {
			offset := uint32(n * 2)
			if miss {
				offset++
			}
			b.Run(fmt.Sprintf("indexed/%d/miss=%v", n, miss), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					gcCallsiteBenchmarkSink = plan.callsiteIndexByReturnOffset(offset)
				}
			})
			b.Run(fmt.Sprintf("linear/%d/miss=%v", n, miss), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					index := -1
					for j := range plan.callsites {
						if plan.callsites[j].returnOffset == offset {
							index = j
							break
						}
					}
					gcCallsiteBenchmarkSink = index
				}
			})
		}
	}
}
