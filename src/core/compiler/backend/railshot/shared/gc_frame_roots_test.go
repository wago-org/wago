package shared

import (
	"testing"
	"unsafe"
)

func TestGCFrameRootPlanFootprint(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit fixed-footprint assertion")
	}
	if got := unsafe.Sizeof(GCFrameRootPlan{}); got != 208 {
		t.Fatalf("GCFrameRootPlan size=%d, want 208", got)
	}
	if got := unsafe.Sizeof(GCModuleFrameRootPlan{}); got != 40 {
		t.Fatalf("GCModuleFrameRootPlan size=%d, want 40", got)
	}
}

func TestGCFrameRootPlanWideMaskWords(t *testing.T) {
	plan := &GCFrameRootPlan{
		LocalOffsets:       make([]uint32, 65),
		LiveLocalMasks:     []uint64{1},
		LiveCallLocalMasks: []uint64{2},
		LiveMaskExtraWords: []uint64{1, 1},
	}
	if !plan.ValidLiveMasks() {
		t.Fatal("valid 65-root two-word plan rejected")
	}
	if !plan.LocalLiveAt(0, 0) || !plan.LocalLiveAt(0, 64) || plan.LocalLiveAt(0, 1) {
		t.Fatal("allocation mask lookup returned the wrong roots")
	}
	if !plan.CallLocalLiveAt(0, 1) || !plan.CallLocalLiveAt(0, 64) || plan.CallLocalLiveAt(0, 0) {
		t.Fatal("call mask lookup returned the wrong roots")
	}
	plan.LiveMaskExtraWords = plan.LiveMaskExtraWords[:1]
	if plan.ValidLiveMasks() {
		t.Fatal("truncated two-word allocation arena accepted")
	}
}

func TestGCFrameRootPlanTrackedVectorMask(t *testing.T) {
	const roots = GCFrameRootLimit + 1
	extraWords := (roots+63)/64 - 1
	plan := &GCFrameRootPlan{
		LocalOffsets:       make([]uint32, roots),
		LiveLocalMasks:     []uint64{0},
		LiveMaskExtraWords: make([]uint64, extraWords),
	}
	plan.LiveMaskExtraWords[extraWords-1] = 1
	if !plan.ValidLiveMasks() || !plan.LocalLiveAt(0, roots-1) {
		t.Fatal("bounded vector mask lost its highest root")
	}
	plan.LocalOffsets = make([]uint32, GCFrameTrackedLocalLimit+1)
	if plan.ValidLiveMasks() {
		t.Fatal("over-limit vector mask accepted")
	}
}
