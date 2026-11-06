//go:build amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestActiveChunkSurvivesGrowthResetAndTrim(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	nodes := int(shared.MaxRetainedStackArenaBytes/uint64(unsafe.Sizeof(elem{}))) + 2*maxStackChunkCap
	headerMoves := 0
	for _, count := range []int{nodes, 3, nodes} {
		var anchors []*elem
		for i := 0; i < count; i++ {
			oldHeaders := &s.chunks[0]
			oldChunk := s.cur
			e := s.alloc()
			if s.activeChunk != &s.chunks[s.cur] {
				t.Fatalf("node %d: active header is detached after growth", i)
			}
			if oldHeaders != &s.chunks[0] {
				headerMoves++
			}
			if *e != (elem{}) {
				t.Fatalf("node %d: reused allocation is not zeroed", i)
			}
			e.st.cval = int64(i + 1)
			if oldChunk != s.cur || i == 0 {
				anchors = append(anchors, e)
			}
		}
		for _, e := range anchors {
			if e.st.cval == 0 {
				t.Fatal("growing chunk headers invalidated a live operand")
			}
		}
		s.finishFunction()
		if s.activeChunk != &s.chunks[s.cur] {
			t.Fatal("trimming chunks retained a detached active header")
		}
		s.reset()
		if s.cur != 0 || s.activeChunk != &s.chunks[0] {
			t.Fatal("reset did not restore the first active chunk")
		}
	}
	if headerMoves < 3 {
		t.Fatalf("only %d header moves; test did not exercise backing replacement", headerMoves)
	}
}

func TestWorkerCloseReleasesActiveChunk(t *testing.T) {
	sc := newCompileScratch(minStackArenaCap)
	sc.ensureTargetStack()
	if sc.stack.activeChunk == nil {
		t.Fatal("initial active header is absent")
	}
	sc.finishStackWorker()
	if sc.stack.activeChunk != nil || sc.stack.chunks != nil {
		t.Fatal("worker teardown retained operand chunk headers")
	}
}

func TestTryAllocFullDoesNotAdvance(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	for i := 1; i < minStackArenaCap; i++ {
		if s.tryAlloc() == nil {
			t.Fatalf("early full chunk at node %d", i)
		}
	}
	header := s.activeChunk
	for range 3 {
		if s.tryAlloc() != nil || s.cur != 0 || len(s.chunks) != 1 || s.activeChunk != header || len(*header) != minStackArenaCap {
			t.Fatal("full fast allocation advanced or modified the arena")
		}
	}
	e := s.allocNextChunk()
	if e != &s.chunks[1][0] || *e != (elem{}) || s.activeChunk != &s.chunks[1] {
		t.Fatal("slow allocation did not publish the first fresh node")
	}
}
