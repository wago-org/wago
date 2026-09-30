package wago

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcFrameLivenessBenchmarkBody(n int) []byte {
	body := make([]byte, 0, n*3)
	for i := 0; i < n; i++ {
		body = append(body, 0x20, 0x00, 0x1a) // local.get 0; drop
		if i&15 == 0 {
			body = append(body, 0xfb, 0x01, 0x00, 0x1a) // struct.new_default 0; drop
		}
	}
	return append(body, 0x0b)
}

func gcFrameTestLocals(indexes, offsets []uint32) []shared.GCFrameLocal {
	locals := make([]shared.GCFrameLocal, len(indexes))
	for i, index := range indexes {
		locals[i].Index = index
		if i < len(offsets) {
			locals[i].Offset = offsets[i]
		}
	}
	return locals
}

func gcFrameTestRootPlan(locals []shared.GCFrameLocal, masks gcFrameLiveMasks) shared.GCFrameRootPlan {
	plan := shared.GCFrameRootPlan{Locals: locals}
	if !plan.SetLiveMasks(masks.words, masks.allocationN, masks.callN) {
		panic("invalid test root masks")
	}
	return plan
}

func TestGCFrameConservativeModeAdmissionIsBounded(t *testing.T) {
	for roots := 0; roots <= gcFrameConservativeLocalLimit+1; roots++ {
		if got, want := gcFramePreferConservativeMasks(roots, 1), roots <= gcFrameConservativeLocalLimit; got != want {
			t.Fatalf("roots %d conservative admission = %v, want %v", roots, got, want)
		}
	}
	if gcFramePreferConservativeMasks(-1, 1) || gcFramePreferConservativeMasks(1, -1) {
		t.Fatal("negative root or body count admitted")
	}
	if !gcFramePreferConservativeMasks(0, gcFrameConservativeRootByteLimit*2) {
		t.Fatal("root-free function rejected by conservative site-counting path")
	}
	if gcFramePreferConservativeMasks(4, gcFrameConservativeRootByteLimit/4+1) {
		t.Fatal("root-byte budget overflow admitted")
	}
}

func TestGCFrameLocalLivenessIsArchitectureIndependent(t *testing.T) {
	tests := []struct {
		name    string
		body    []byte
		indexes []uint32
		calls   bool
		want    []uint64
	}{
		{
			name:    "live after allocation",
			body:    []byte{0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0x0b},
			indexes: []uint32{0},
			want:    []uint64{1},
		},
		{
			name:    "dead before allocation",
			body:    []byte{0x20, 0x00, 0x1a, 0xfb, 0x01, 0x00, 0x1a, 0x0b},
			indexes: []uint32{0},
			want:    []uint64{0},
		},
		{
			name:    "native call",
			body:    []byte{0x10, 0x00, 0x20, 0x01, 0x1a, 0x0b},
			indexes: []uint32{0, 1},
			calls:   true,
			want:    []uint64{2},
		},
		{
			name: "if successors",
			body: []byte{
				0x20, 0x00, 0x04, 0x40, // local.get 0; if
				0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
				0x05,                   // else
				0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
				0x0b, 0x20, 0x01, 0x1a, 0x0b,
			},
			indexes: []uint32{0, 1},
			want:    []uint64{2, 2},
		},
		{
			name: "loop backedge",
			body: []byte{
				0x03, 0x40, 0x10, 0x00, // loop; call 0
				0x20, 0x00, 0x0d, 0x00, // local.get 0; br_if 0
				0x0b, 0x20, 0x01, 0x1a, 0x0b,
			},
			indexes: []uint32{0, 1},
			calls:   true,
			want:    []uint64{3},
		},
		{
			name: "br_table target arena",
			body: []byte{
				0x02, 0x40, 0x02, 0x40, 0x10, 0x00, // block; block; call 0
				0x20, 0x00, 0x0e, 0x01, 0x00, 0x01, // local.get 0; br_table 0 1
				0x0b, 0x0b, 0x20, 0x01, 0x1a, 0x0b,
			},
			indexes: []uint32{0, 1},
			calls:   true,
			want:    []uint64{3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			masks, err := gcFrameLocalLivenessArena(tc.body, gcFrameTestLocals(tc.indexes, nil))
			if err != nil {
				t.Fatal(err)
			}
			start, count := 0, masks.allocationN
			if tc.calls {
				start, count = masks.allocationN, masks.callN
			}
			if count != len(tc.want) {
				t.Fatalf("mask count = %d, want %d", count, len(tc.want))
			}
			for i := range tc.want {
				got := uint64(0)
				words := masks.site(start + i)
				if masks.rootIndexes == nil {
					got = words[0]
				} else {
					for liveRoot, inputRoot := range masks.rootIndexes {
						if words[liveRoot/64]&(uint64(1)<<uint(liveRoot%64)) != 0 {
							got |= uint64(1) << inputRoot
						}
					}
				}
				if got != tc.want[i] {
					t.Fatalf("mask %d = %#x, want %#x", i, got, tc.want[i])
				}
			}
		})
	}
}

func appendGCTestU32(dst []byte, value uint32) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

func gcFrameWideLivenessBody(rootCount int) []byte {
	body := []byte{0xfb, 0x01, 0x00, 0x1a, 0x10, 0x00} // allocation; drop; call 0
	for i := 0; i < rootCount; i++ {
		body = append(body, 0x20)
		body = appendGCTestU32(body, uint32(i))
		body = append(body, 0x1a)
	}
	return append(body, 0x0b)
}

func TestGCFrameLocalLivenessWideMasks(t *testing.T) {
	for _, roots := range []int{64, 65, 128, 256, 1024} {
		t.Run(fmt.Sprintf("roots=%d", roots), func(t *testing.T) {
			indexes := make([]uint32, roots)
			for i := range indexes {
				indexes[i] = uint32(i)
			}
			masks, err := gcFrameLocalLivenessArena(gcFrameWideLivenessBody(roots), gcFrameTestLocals(indexes, nil))
			if err != nil {
				t.Fatal(err)
			}
			if masks.allocationN != 1 || masks.callN != 1 {
				t.Fatalf("mask counts=%d/%d, want 1/1", masks.allocationN, masks.callN)
			}
			wordCount := (roots + 63) / 64
			if len(masks.words) != wordCount*2 {
				t.Fatalf("mask words=%d, want %d", len(masks.words), wordCount*2)
			}
			plan := gcFrameTestRootPlan(make([]shared.GCFrameLocal, roots), masks)
			if !plan.ValidLiveMasks() {
				t.Fatal("wide mask arena rejected")
			}
			for i := 0; i < roots; i++ {
				if !plan.LocalLiveAt(0, i) || !plan.CallLocalLiveAt(0, i) {
					t.Fatalf("root %d is not live", i)
				}
			}
		})
	}
}

func TestGCFrameLocalLivenessCompactsDeadDeclaredRoots(t *testing.T) {
	const roots = 1138
	indexes := make([]uint32, roots)
	offsets := make([]uint32, roots)
	for i := range indexes {
		indexes[i], offsets[i] = uint32(i), uint32(16+i*8)
	}
	body := []byte{0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0x0b}
	locals := gcFrameTestLocals(indexes, offsets)
	masks, err := gcFrameLocalLivenessArena(body, locals)
	if err != nil {
		t.Fatal(err)
	}
	var maximum int
	locals, masks, maximum, err = gcFrameCompactLiveLocalsArena(locals, masks)
	if err != nil {
		t.Fatal(err)
	}
	if maximum != 1 || len(locals) != 1 || locals[0].Index != 0 || locals[0].Offset != 16 || len(masks.words) != 1 {
		t.Fatalf("compacted roots: maximum=%d locals=%v words=%d", maximum, locals, len(masks.words))
	}
	plan := gcFrameTestRootPlan(locals, masks)
	if !plan.ValidLiveMasks() || !plan.LocalLiveAt(0, 0) {
		t.Fatalf("compacted plan = %+v", plan)
	}
}

func TestGCFrameLocalLivenessStoresOnlyReferencedRoots(t *testing.T) {
	const roots = 1138
	indexes := make([]uint32, roots)
	for i := range indexes {
		indexes[i] = uint32(i)
	}
	body := []byte{0xfb, 0x01, 0x00, 0x1a, 0x10, 0x00, 0x20}
	body = appendGCTestU32(body, roots-1)
	body = append(body, 0x1a, 0x0b)

	locals := gcFrameTestLocals(indexes, nil)
	masks, err := gcFrameLocalLivenessArena(body, locals)
	if err != nil {
		t.Fatal(err)
	}
	if masks.wordsPerSite != 1 || len(masks.words) != 2 || len(masks.rootIndexes) != 1 || masks.rootIndexes[0] != roots-1 {
		t.Fatalf("sparse liveness arena: width=%d words=%d root indexes=%v", masks.wordsPerSite, len(masks.words), masks.rootIndexes)
	}
	if masks.site(0)[0] != 1 || masks.site(1)[0] != 1 {
		t.Fatalf("last root is not live at both sites: %#x / %#x", masks.site(0)[0], masks.site(1)[0])
	}

	locals, masks, maximum, err := gcFrameCompactLiveLocalsArena(locals, masks)
	if err != nil {
		t.Fatal(err)
	}
	plan := gcFrameTestRootPlan(locals, masks)
	if maximum != 1 || len(locals) != 1 || locals[0].Index != uint32(roots-1) || !plan.ValidLiveMasks() {
		t.Fatalf("compacted sparse roots: maximum=%d locals=%v masks=%+v", maximum, locals, masks)
	}
}

func TestGCFrameLocalLivenessNestedLoopFixedPoint(t *testing.T) {
	for _, depth := range []int{8, 32, 128, 512} {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			body := gcFrameNestedLoopLivenessBody(depth)
			masks, err := gcFrameLocalLivenessArena(body, []shared.GCFrameLocal{{Index: 0}})
			if err != nil {
				t.Fatal(err)
			}
			if masks.allocationN != 1 || masks.wordsPerSite != 1 || masks.site(0)[0]&1 == 0 {
				t.Fatalf("nested loop allocation mask = %+v, want root 0 live", masks)
			}
		})
	}
}

func TestGCFrameLocalLivenessCompactsDisjointWideRoots(t *testing.T) {
	const roots = 4096
	locals, masks := gcFrameDisjointRootMasks(roots)
	locals, masks, maximum, err := gcFrameCompactLiveLocalsArena(locals, masks)
	if err != nil {
		t.Fatal(err)
	}
	if len(locals) != roots || maximum != 1 {
		t.Fatalf("compacted disjoint roots: locals=%d maximum=%d", len(locals), maximum)
	}
	plan := gcFrameTestRootPlan(locals, masks)
	for _, root := range []int{0, 63, 64, roots - 1} {
		if !plan.LocalLiveAt(root, root) {
			t.Fatalf("site %d lost its only live root", root)
		}
	}
}

func TestGCFrameLocalLivenessCompactionAcceptsMoreThan1024LiveRoots(t *testing.T) {
	const roots = 1025
	indexes := make([]uint32, roots)
	offsets := make([]uint32, roots)
	masks := newGCFrameLiveMasks(1, 0, (roots+63)/64)
	for i := range indexes {
		indexes[i], offsets[i] = uint32(i), uint32(i*8)
	}
	for i := range masks.words {
		masks.words[i] = ^uint64(0)
	}
	masks.words[len(masks.words)-1] = 1
	gotLocals, _, maximum, err := gcFrameCompactLiveLocalsArena(gcFrameTestLocals(indexes, offsets), masks)
	if err != nil || len(gotLocals) != roots || maximum != roots {
		t.Fatalf("wide simultaneous-root compaction = locals %d maximum %d error %v", len(gotLocals), maximum, err)
	}
}

func TestGCFrameLocalLivenessConsumesMixedWidthMemarg(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1, Addr64: true}}}}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	body := []byte{
		0x42, 0x00, // i64.const 0
		0xfd, 0x00, 0x44, 0x01, 0x80, 0x80, 0x80, 0x80, 0x10, // v128.load memory 1, offset 2^32
		0x1a,       // drop
		0x20, 0x00, // local.get 0
		0x1a, 0x0b,
	}
	if _, err := gcFrameLocalLivenessArenaWithClassifier(body, []shared.GCFrameLocal{{Index: 0}}, &classifier); err != nil {
		t.Fatalf("mixed-width GC liveness walk: %v", err)
	}
}

func TestGCFrameLocalLivenessRejectsUnrepresentableBrTable(t *testing.T) {
	body := []byte{0x0e, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x0b}
	_, err := gcFrameLocalLivenessArena(body, nil)
	if err == nil || !strings.Contains(err.Error(), "br_table target count exceeds implementation limit") {
		t.Fatalf("error = %v, want target-count implementation-limit rejection", err)
	}
}

func TestGCFrameLocalLivenessDeduplicatesRepeatedWideBranchTable(t *testing.T) {
	const roots, targets = 30_000, 100_000
	indexes := make([]uint32, roots)
	for i := range indexes {
		indexes[i] = uint32(i)
	}
	if _, err := gcFrameLocalLivenessArena(gcFrameRepeatedBranchTableBody(targets), gcFrameTestLocals(indexes, nil)); err != nil {
		t.Fatalf("repeated-target br_table liveness: %v", err)
	}
}

func TestGCFrameLocalLivenessExcludesUnreachableSites(t *testing.T) {
	body := []byte{
		0x00,                   // unreachable
		0xfb, 0x01, 0x00, 0x1a, // unreachable struct.new_default 0; drop
		0x10, 0x00, // unreachable call 0
		0x0b,
	}
	masks, err := gcFrameLocalLivenessArena(body, []shared.GCFrameLocal{{Index: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if masks.allocationN != 0 || masks.callN != 0 || len(masks.words) != 0 {
		t.Fatalf("unreachable masks = allocations %d calls %d words %d", masks.allocationN, masks.callN, len(masks.words))
	}
}

func TestGCFrameLocalLivenessBudgetsDistinctWideBranchTableEdges(t *testing.T) {
	const roots, targets = 8192, 20_000
	indexes := make([]uint32, roots)
	body := make([]byte, 0, roots*3+targets*6)
	for i := range indexes {
		indexes[i] = uint32(i)
		body = append(body, 0x20)
		body = appendGCTestU32(body, uint32(i))
		body = append(body, 0x1a) // keep all roots in the tracked analysis width
	}
	body = append(body, gcFrameDistinctBranchTableBody(targets)...)
	_, err := gcFrameLocalLivenessArena(body, gcFrameTestLocals(indexes, nil))
	if err == nil || !strings.Contains(err.Error(), "graph exceeds 8388608 bitmap-word implementation limit") {
		t.Fatalf("distinct-target br_table liveness error = %v", err)
	}
}

func TestGCFrameLocalLivenessAllocationBudget(t *testing.T) {
	if got := unsafe.Sizeof(gcLiveNode{}); got != 32 {
		t.Fatalf("gcLiveNode size = %d, want 32 bytes", got)
	}
	if got := unsafe.Sizeof(gcFrameLiveMasks{}); got != 72 {
		t.Fatalf("gcFrameLiveMasks size = %d, want 72 bytes", got)
	}
	body := gcFrameLivenessBenchmarkBody(1024)
	allocs := testing.AllocsPerRun(5, func() {
		if _, err := gcFrameLocalLivenessArena(body, []shared.GCFrameLocal{{Index: 0}}); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 100 {
		t.Fatalf("GC frame liveness allocations = %.0f, want at most 100", allocs)
	}
}

func TestGCFrameLocalLivenessArenaBudget(t *testing.T) {
	const words = (1138 + 63) / 64
	boundary := maxGCFrameLivenessArenaBytes / 8 / words
	if !gcFrameLivenessArenaFits(boundary, words) {
		t.Fatal("liveness arena rejected its exact 64 MiB boundary")
	}
	if gcFrameLivenessArenaFits(boundary+1, words) {
		t.Fatal("liveness arena accepted a byte above its 64 MiB boundary")
	}
	if !gcFrameLivenessWorkFits(1, boundary-1, words) {
		t.Fatal("liveness work rejected its exact 64 MiB-equivalent boundary")
	}
	if gcFrameLivenessWorkFits(1, boundary, words) {
		t.Fatal("liveness work accepted one unit above its 64 MiB-equivalent boundary")
	}
}

func TestGCFramePredecessorArenaBudget(t *testing.T) {
	if !gcFramePredecessorArenaFits(1, (maxGCFrameLivenessArenaBytes-13)/4) {
		t.Fatal("predecessor arena rejected its computed boundary")
	}
	if gcFramePredecessorArenaFits(1, (maxGCFrameLivenessArenaBytes-13)/4+1) {
		t.Fatal("predecessor arena accepted one edge above its boundary")
	}
	if gcFramePredecessorArenaFits(-1, 0) || gcFramePredecessorArenaFits(1, -1) {
		t.Fatal("predecessor arena accepted negative dimensions")
	}
}

func TestGCFrameConservativeLivenessAcceptsMoreThan1024Roots(t *testing.T) {
	masks, err := gcFrameAllLiveMasksArena([]byte{0x0b}, 1025)
	if err != nil || masks.allocationN != 0 || masks.callN != 0 {
		t.Fatalf("wide conservative roots = %d allocations, %d calls, %v", masks.allocationN, masks.callN, err)
	}
}

func TestGCFrameConservativeLivenessArenaBudget(t *testing.T) {
	const roots = shared.GCFrameTrackedLocalLimit
	wordCount := (roots + 63) / 64
	boundary := maxGCFrameLivenessArenaBytes / 8 / wordCount
	body := make([]byte, 0, boundary*2+3)
	for range boundary + 1 {
		body = append(body, 0x10, 0x00) // call 0
	}
	body = append(body, 0x0b)
	if _, err := gcFrameAllLiveMasksArena(body, roots); err == nil || !strings.Contains(err.Error(), "mask arena exceeds") {
		t.Fatalf("conservative mask arena error = %v, want bounded rejection", err)
	}
}

func BenchmarkGCFrameLocalLivenessRootCounts(b *testing.B) {
	for _, roots := range []int{64, 65, 128, 256, 1024} {
		b.Run(fmt.Sprintf("roots=%d", roots), func(b *testing.B) {
			body := gcFrameWideLivenessBody(roots)
			indexes := make([]uint32, roots)
			for i := range indexes {
				indexes[i] = uint32(i)
			}
			locals := gcFrameTestLocals(indexes, nil)
			b.ReportAllocs()
			b.ReportMetric(float64((roots+63)/64), "words/site")
			for i := 0; i < b.N; i++ {
				if _, err := gcFrameLocalLivenessArena(body, locals); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkGCFrameLocalLivenessSparseDeclaredRoots(b *testing.B) {
	const roots = 1138
	indexes := make([]uint32, roots)
	offsets := make([]uint32, roots)
	for i := range indexes {
		indexes[i], offsets[i] = uint32(i), uint32(16+i*8)
	}
	locals := gcFrameTestLocals(indexes, offsets)
	body := []byte{0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0x0b}
	wordsPerSite := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		masks, err := gcFrameLocalLivenessArena(body, locals)
		if err != nil {
			b.Fatal(err)
		}
		compacted, _, maximum, err := gcFrameCompactLiveLocalsArena(locals, masks)
		if err != nil || len(compacted) != 1 || maximum != 1 {
			b.Fatalf("sparse compaction locals=%d maximum=%d err=%v", len(compacted), maximum, err)
		}
		wordsPerSite = masks.wordsPerSite
	}
	b.ReportMetric(float64(wordsPerSite), "words/site")
}

func BenchmarkGCFrameLocalLivenessSparseProduct(b *testing.B) {
	for _, size := range []int{2048, 4096, 8192} {
		b.Run(fmt.Sprintf("roots=%d/sites=%d", size, size), func(b *testing.B) {
			indexes := make([]uint32, size)
			offsets := make([]uint32, size)
			for i := range indexes {
				indexes[i], offsets[i] = uint32(i), uint32(16+i*8)
			}
			locals := gcFrameTestLocals(indexes, offsets)
			body := make([]byte, 0, size*2+8)
			for range size {
				body = append(body, 0x10, 0x00) // call 0
			}
			body = append(body, 0x20)
			body = appendGCTestU32(body, uint32(size-1))
			body = append(body, 0x1a, 0x0b)
			b.ReportAllocs()
			var wordsPerSite int
			for i := 0; i < b.N; i++ {
				masks, err := gcFrameLocalLivenessArena(body, locals)
				if err != nil {
					b.Fatal(err)
				}
				if masks.callN != size {
					b.Fatalf("call sites=%d, want %d", masks.callN, size)
				}
				wordsPerSite = masks.wordsPerSite
			}
			b.ReportMetric(float64(wordsPerSite), "words/site")
		})
	}
}

func BenchmarkGCFrameCompactDisjointWideRoots(b *testing.B) {
	const roots = 10_000
	locals, masks := gcFrameDisjointRootMasks(roots)
	b.ReportAllocs()
	b.ReportMetric(float64(len(masks.words)*8), "input-arena-bytes")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		locals, masks, _, err = gcFrameCompactLiveLocalsArena(locals, masks)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func gcFrameDisjointRootMasks(roots int) (locals []shared.GCFrameLocal, masks gcFrameLiveMasks) {
	locals = make([]shared.GCFrameLocal, roots)
	masks = newGCFrameLiveMasks(roots, 0, (roots+63)/64)
	for root := 0; root < roots; root++ {
		locals[root] = shared.GCFrameLocal{Index: uint32(root), Offset: uint32(16 + root*8)}
		masks.site(root)[root/64] = uint64(1) << uint(root%64)
	}
	return
}

func BenchmarkGCFrameLocalLiveness(b *testing.B) {
	body := gcFrameLivenessBenchmarkBody(16 * 1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		masks, err := gcFrameLocalLivenessArena(body, []shared.GCFrameLocal{{Index: 0}})
		if err != nil {
			b.Fatal(err)
		}
		if masks.allocationN != 1024 || masks.callN != 0 {
			b.Fatalf("mask counts = %d allocations, %d calls; want 1024, 0", masks.allocationN, masks.callN)
		}
	}
}

func BenchmarkGCFrameLocalLivenessNestedLoops(b *testing.B) {
	for _, depth := range []int{8, 16, 32, 64} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			body := gcFrameNestedLoopLivenessBody(depth)
			locals := []shared.GCFrameLocal{{Index: 0}}
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				if _, err := gcFrameLocalLivenessArena(body, locals); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func gcFrameNestedLoopLivenessBody(depth int) []byte {
	body := make([]byte, 0, depth*5+16)
	for i := 0; i < depth; i++ {
		body = append(body, 0x03, 0x40) // loop (empty block type)
	}
	body = append(body, 0xfb, 0x01, 0x00, 0x1a) // allocate; drop
	body = append(body, 0x20, 0x00, 0x1a)       // local.get 0; drop
	for i := 0; i < depth; i++ {
		body = append(body, 0x41, 0x00, 0x0d, 0x00, 0x0b) // i32.const 0; br_if 0; end
	}
	return append(body, 0x0b)
}

func BenchmarkGCFrameLocalLivenessRepeatedWideBranchTable(b *testing.B) {
	const roots, targets = 30_000, 250_000
	indexes := make([]uint32, roots)
	for i := range indexes {
		indexes[i] = uint32(i)
	}
	locals := gcFrameTestLocals(indexes, nil)
	body := gcFrameRepeatedBranchTableBody(targets)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(targets, "raw-edges/op")
	for i := 0; i < b.N; i++ {
		if _, err := gcFrameLocalLivenessArena(body, locals); err != nil {
			b.Fatal(err)
		}
	}
}

func gcFrameRepeatedBranchTableBody(targets int) []byte {
	body := make([]byte, 0, targets+16)
	body = append(body, 0x02, 0x40, 0x41, 0x00, 0x0e) // block; i32.const 0; br_table
	body = append(body, wasmtest.ULEB(uint32(targets-1))...)
	for i := 0; i < targets; i++ { // vector targets plus the default target
		body = append(body, 0x00)
	}
	return append(body, 0x0b, 0x0b)
}

func gcFrameDistinctBranchTableBody(targets int) []byte {
	body := make([]byte, 0, targets*6)
	for i := 0; i < targets; i++ {
		body = append(body, 0x02, 0x40) // block
	}
	body = append(body, 0x41, 0x00, 0x0e) // i32.const 0; br_table
	body = append(body, wasmtest.ULEB(uint32(targets-1))...)
	for depth := 0; depth < targets; depth++ { // vector targets plus default
		body = append(body, wasmtest.ULEB(uint32(depth))...)
	}
	for i := 0; i < targets; i++ {
		body = append(body, 0x0b)
	}
	return append(body, 0x0b)
}
