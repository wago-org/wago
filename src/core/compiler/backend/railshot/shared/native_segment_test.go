package shared

import "testing"

func TestAnalyzeNativeSegment(t *testing.T) {
	boundary := NativeSegmentNode{Work: 1, Boundary: true}
	for _, tc := range []struct {
		name        string
		nodes       []NativeSegmentNode
		limit, want uint32
		ok          bool
	}{
		{"park", []NativeSegmentNode{boundary}, 1, 1, true},
		{"branches", []NativeSegmentNode{{Work: 2, Successors: []uint32{1, 2}}, {Work: 3, Successors: []uint32{2}}, boundary}, 6, 6, true},
		{"hidden-loop", []NativeSegmentNode{{Work: 1, Successors: []uint32{1, 2}}, {Work: 1, Successors: []uint32{1}}, boundary}, 100, 0, false},
		{"recursive-cycle", []NativeSegmentNode{{Work: 1, Successors: []uint32{1}}, {Work: 1, Successors: []uint32{0}}}, 100, 0, false},
		{"unknown-helper", []NativeSegmentNode{{Successors: []uint32{1}}, boundary}, 100, 0, false},
		{"unknown-target", []NativeSegmentNode{{Work: 1, Successors: []uint32{99}}}, 100, 0, false},
		{"callee-return-unproved", []NativeSegmentNode{{Work: 1}}, 100, 0, false},
		{"conflicting-boundary", []NativeSegmentNode{{Work: 1, Boundary: true, Successors: []uint32{0}}}, 100, 0, false},
		{"overflow", []NativeSegmentNode{{Work: ^uint32(0), Successors: []uint32{1}}, boundary}, ^uint32(0), 0, false},
		{"work-limit", []NativeSegmentNode{{Work: 2, Successors: []uint32{1}}, boundary}, 2, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AnalyzeNativeSegment(tc.nodes, 0, tc.limit)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("bound=%d/%v want %d/%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAnalyzeNativeSegmentBoundsAnalysis(t *testing.T) {
	nodes := make([]NativeSegmentNode, maxNativeSegmentNodes+1)
	if _, ok := AnalyzeNativeSegment(nodes, 0, 100); ok {
		t.Fatal("excess nodes accepted")
	}
	nodes = nodes[:maxNativeSegmentDepth+1]
	for i := range nodes {
		nodes[i] = NativeSegmentNode{Work: 1, Successors: []uint32{uint32(i + 1)}}
	}
	nodes[len(nodes)-1] = NativeSegmentNode{Work: 1, Boundary: true}
	if _, ok := AnalyzeNativeSegment(nodes, 0, 1000); ok {
		t.Fatal("excess depth accepted")
	}
	nodes = []NativeSegmentNode{{Work: 1, Successors: make([]uint32, maxNativeSegmentEdges+1)}}
	if _, ok := AnalyzeNativeSegment(nodes, 0, 100); ok {
		t.Fatal("excess edges accepted")
	}
}

func TestNativeSegmentEntryDoesNotProveContinuation(t *testing.T) {
	nodes := []NativeSegmentNode{{Work: 2, Successors: []uint32{1}}, {Work: 1, Boundary: true}, {Work: 1, Successors: []uint32{2}}}
	if _, ok := AnalyzeNativeSegment(nodes, 0, 10); !ok {
		t.Fatal("bounded prefix rejected")
	}
	if _, ok := AnalyzeNativeSegment(nodes, 2, 10); ok {
		t.Fatal("prefix proof admitted looping continuation")
	}
}

func BenchmarkAnalyzeNativeSegment(b *testing.B) {
	nodes := make([]NativeSegmentNode, 64)
	for i := range nodes {
		nodes[i] = NativeSegmentNode{Work: 1, Successors: []uint32{uint32(i + 1)}}
	}
	nodes[63] = NativeSegmentNode{Work: 1, Boundary: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := AnalyzeNativeSegment(nodes, 0, 64); !ok {
			b.Fatal("bounded graph rejected")
		}
	}
}
