package wago

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
			var callMasks []uint64
			got, err := gcFrameLocalLiveness(tc.body, tc.indexes, &callMasks, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.calls {
				got = callMasks
			}
			if len(got) != len(tc.want) {
				t.Fatalf("mask count = %d, want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("mask %d = %#x, want %#x", i, got[i], tc.want[i])
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
			var calls []uint64
			var extra gcFrameLivenessExtra
			allocations, err := gcFrameLocalLiveness(gcFrameWideLivenessBody(roots), indexes, &calls, &extra)
			if err != nil {
				t.Fatal(err)
			}
			if len(allocations) != 1 || len(calls) != 1 {
				t.Fatalf("mask counts=%d/%d, want 1/1", len(allocations), len(calls))
			}
			extraWords := (roots+63)/64 - 1
			if len(extra.words) != extraWords*2 {
				t.Fatalf("extra words=%d, want %d", len(extra.words), extraWords*2)
			}
			plan := shared.GCFrameRootPlan{
				LocalOffsets:       make([]uint32, roots),
				LiveLocalMasks:     allocations,
				LiveCallLocalMasks: calls,
				LiveMaskExtraWords: extra.words,
			}
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
	var calls []uint64
	var extra gcFrameLivenessExtra
	allocations, err := gcFrameLocalLiveness(body, indexes, &calls, &extra)
	if err != nil {
		t.Fatal(err)
	}
	indexes, offsets, allocations, calls, maximum, err := gcFrameCompactLiveLocals(indexes, offsets, allocations, calls, &extra)
	if err != nil {
		t.Fatal(err)
	}
	if maximum != 1 || len(indexes) != 1 || indexes[0] != 0 || len(offsets) != 1 || len(extra.words) != 0 {
		t.Fatalf("compacted roots: maximum=%d indexes=%v offsets=%v extra=%d", maximum, indexes, offsets, len(extra.words))
	}
	plan := shared.GCFrameRootPlan{LocalOffsets: offsets, LiveLocalMasks: allocations, LiveCallLocalMasks: calls, LiveMaskExtraWords: extra.words}
	if !plan.ValidLiveMasks() || !plan.LocalLiveAt(0, 0) {
		t.Fatalf("compacted plan = %+v", plan)
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
	var calls []uint64
	if _, err := gcFrameLocalLivenessWithClassifier(body, []uint32{0}, &calls, nil, &classifier); err != nil {
		t.Fatalf("mixed-width GC liveness walk: %v", err)
	}
}

func TestGCFrameLocalLivenessRejectsUnrepresentableBrTable(t *testing.T) {
	body := []byte{0x0e, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x0b}
	_, err := gcFrameLocalLiveness(body, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "br_table target count exceeds implementation limit") {
		t.Fatalf("error = %v, want target-count implementation-limit rejection", err)
	}
}

func TestGCFrameLocalLivenessAllocationBudget(t *testing.T) {
	if got := unsafe.Sizeof(gcLiveNode{}); got != 32 {
		t.Fatalf("gcLiveNode size = %d, want 32 bytes", got)
	}
	if got := unsafe.Sizeof(gcFrameLivenessExtra{}); got != 24 {
		t.Fatalf("gcFrameLivenessExtra size = %d, want 24 bytes", got)
	}
	body := gcFrameLivenessBenchmarkBody(1024)
	allocs := testing.AllocsPerRun(5, func() {
		var callMasks []uint64
		if _, err := gcFrameLocalLiveness(body, []uint32{0}, &callMasks, nil); err != nil {
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
}

func BenchmarkGCFrameLocalLivenessRootCounts(b *testing.B) {
	for _, roots := range []int{64, 65, 128, 256, 1024} {
		b.Run(fmt.Sprintf("roots=%d", roots), func(b *testing.B) {
			body := gcFrameWideLivenessBody(roots)
			indexes := make([]uint32, roots)
			for i := range indexes {
				indexes[i] = uint32(i)
			}
			b.ReportAllocs()
			b.ReportMetric(float64((roots+63)/64), "words/site")
			for i := 0; i < b.N; i++ {
				var calls []uint64
				var extra gcFrameLivenessExtra
				if _, err := gcFrameLocalLiveness(body, indexes, &calls, &extra); err != nil {
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
	body := []byte{0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0x0b}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var calls []uint64
		var extra gcFrameLivenessExtra
		allocations, err := gcFrameLocalLiveness(body, indexes, &calls, &extra)
		if err != nil {
			b.Fatal(err)
		}
		_, compacted, _, _, maximum, err := gcFrameCompactLiveLocals(indexes, offsets, allocations, calls, &extra)
		if err != nil || len(compacted) != 1 || maximum != 1 {
			b.Fatalf("sparse compaction offsets=%d maximum=%d err=%v", len(compacted), maximum, err)
		}
	}
}

func BenchmarkGCFrameLocalLiveness(b *testing.B) {
	body := gcFrameLivenessBenchmarkBody(16 * 1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		var callMasks []uint64
		masks, err := gcFrameLocalLiveness(body, []uint32{0}, &callMasks, nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(masks) != 1024 || len(callMasks) != 0 {
			b.Fatalf("mask counts = %d allocations, %d calls; want 1024, 0", len(masks), len(callMasks))
		}
	}
}
