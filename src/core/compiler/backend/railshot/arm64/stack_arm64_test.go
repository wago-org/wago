//go:build (linux || darwin) && arm64

package arm64

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// Operand-stack arena sizing, ported from amd64/stack_test.go. The arena-capacity
// heuristics are shared verbatim with amd64 (identical constants and formulas), so
// these guard arm64's copy against drift; the pointer-stability invariant is
// covered by allocation_arm64_test.go.

func TestNewStackArenaDefaultCapacityArm64(t *testing.T) {
	s := newStack()
	if cap(s.chunks[0]) != defaultStackArenaCap {
		t.Fatalf("stack arena cap = %d, want %d", cap(s.chunks[0]), defaultStackArenaCap)
	}
}

func TestNewStackWithCapSizesFirstChunkArm64(t *testing.T) {
	for _, tc := range []struct {
		hint int
		want int
	}{
		{0, minStackArenaCap},
		{minStackArenaCap - 1, minStackArenaCap},
		{minStackArenaCap + 7, minStackArenaCap + 7},
		{defaultStackArenaCap + 1, defaultStackArenaCap + 1},
	} {
		s := newStackWithCap(tc.hint)
		if cap(s.chunks[0]) != tc.want {
			t.Fatalf("newStackWithCap(%d) cap = %d, want %d", tc.hint, cap(s.chunks[0]), tc.want)
		}
		if s.head == nil || s.head.next != sentinelNodeID || s.head.prev != sentinelNodeID {
			t.Fatalf("newStackWithCap(%d) did not initialize sentinel links", tc.hint)
		}
	}
}

func TestStackNodeIDsSurviveChunkGrowthArm64(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	firstID, first := s.alloc()
	for len(s.chunks) == 1 {
		s.alloc()
	}
	secondID, second := s.alloc()
	if firstID == nilNodeID || secondID == nilNodeID || firstID == secondID {
		t.Fatalf("node IDs = %d, %d; want distinct nonzero IDs", firstID, secondID)
	}
	if s.node(firstID) != first || s.node(secondID) != second {
		t.Fatal("node ID changed identity after arena chunk growth")
	}
}

func TestStackCustomSidecarIsLazyAndClearedArm64(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	_, ordinaryReserved := s.nodeMemory()
	e := s.pushValue(storage{kind: stReg, typ: mtCustom})
	s.setElemCold(e, nil, []Reg{1, 2})
	if e.st.cold == 0 || len(s.cold) != 1 {
		t.Fatalf("custom sidecar index=%d len=%d, want nonzero and 1", e.st.cold, len(s.cold))
	}
	if _, reserved := s.nodeMemory(); reserved <= ordinaryReserved {
		t.Fatalf("reserved node memory = %d, want more than ordinary %d", reserved, ordinaryReserved)
	}
	s.reset()
	if len(s.cold) != 0 {
		t.Fatalf("reset custom sidecars = %d, want 0", len(s.cold))
	}
	if stale := s.cold[:cap(s.cold)][0]; stale.custom != nil || stale.vregs != nil {
		t.Fatalf("reset retained custom sidecar pointers: %+v", stale)
	}
}

func TestHintedStackGrowthMatchesLegacyRetentionArm64(t *testing.T) {
	const (
		firstCap = 386
		nodes    = 31_000
	)
	hinted, legacy := newStackWithCap(firstCap), newStack()
	for i := 1; i < nodes; i++ { // each stack already contains its sentinel
		hinted.alloc()
		legacy.alloc()
	}
	if len(hinted.chunks) < 2 || cap(hinted.chunks[1]) != 768-firstCap {
		t.Fatalf("hinted fallback chunks = %v, want second cap %d", stackChunkCapsArm64(hinted), 768-firstCap)
	}
	if got, want := retainedStackArenaCapacityArm64(hinted), retainedStackArenaCapacityArm64(legacy); got != want {
		t.Fatalf("hinted retained capacity = %d, want legacy %d; hinted=%v legacy=%v", got, want, stackChunkCapsArm64(hinted), stackChunkCapsArm64(legacy))
	}
}

func TestSubDefaultHintPreservesGeometricGrowthArm64(t *testing.T) {
	const nodes = 2_000
	s := newStackWithCap(101)
	for i := 1; i < nodes; i++ {
		s.alloc()
	}
	want := []int{101, 202, 404, 808, 1616}
	if got := stackChunkCapsArm64(s); !slices.Equal(got, want) {
		t.Fatalf("sub-default growth = %v, want %v", got, want)
	}
}

func TestStackFinishFunctionRetainsBoundedReusableOverflowArm64(t *testing.T) {
	const nodes = 2_000
	s := newStackWithCap(minStackArenaCap)
	for i := 1; i < nodes; i++ {
		s.alloc()
	}
	wantCapacity := retainedStackArenaCapacityArm64(s)
	if got := uint64(wantCapacity) * uint64(unsafe.Sizeof(elem{})); got >= shared.MaxRetainedStackArenaBytes {
		t.Fatalf("ordinary backing = %d bytes, want below retention limit", got)
	}
	if got := s.finishFunction(); got != 0 {
		t.Fatalf("ordinary function discarded %d bytes, want 0", got)
	}
	overflow := &s.chunks[1][0]

	s.reset()
	for i := 1; i < 4; i++ {
		s.alloc()
	}
	if got := s.finishFunction(); got != 0 {
		t.Fatalf("tiny successor discarded %d bytes within budget", got)
	}

	s.reset()
	for i := 1; i < nodes; i++ {
		s.alloc()
	}
	if got := &s.chunks[1][0]; got != overflow {
		t.Fatal("recurring ordinary demand did not reuse overflow backing")
	}
	if got := s.finishFunction(); got != 0 {
		t.Fatalf("recurring ordinary function discarded %d bytes, want 0", got)
	}

	giantNodes := int(shared.MaxRetainedStackArenaBytes/uint64(unsafe.Sizeof(elem{}))) + maxStackChunkCap
	s.reset()
	for i := 1; i < giantNodes; i++ {
		s.alloc()
	}
	oldChunks := s.chunks
	oldCapacity := retainedStackArenaCapacityArm64(s)
	keepCapacity := 0
	keep := 0
	for i := range s.chunks {
		chunkBytes := uint64(cap(s.chunks[i])) * uint64(unsafe.Sizeof(elem{}))
		if i != 0 && uint64(keepCapacity)*uint64(unsafe.Sizeof(elem{}))+chunkBytes > shared.MaxRetainedStackArenaBytes {
			break
		}
		keepCapacity += cap(s.chunks[i])
		keep = i + 1
	}
	wantDiscarded := uint64(oldCapacity-keepCapacity) * uint64(unsafe.Sizeof(elem{}))
	if got := s.finishFunction(); got != wantDiscarded {
		t.Fatalf("giant overflow discarded %d bytes, want %d", got, wantDiscarded)
	}
	if len(s.chunks) != keep {
		t.Fatalf("retained chunks = %d, want %d", len(s.chunks), keep)
	}
	for i := keep; i < len(oldChunks); i++ {
		if oldChunks[i] != nil {
			t.Fatalf("discarded chunk %d still has a slice header", i)
		}
	}
	if stale := s.chunks[0][:cap(s.chunks[0])][1]; stale.prev != nilNodeID || stale.next != nilNodeID || stale.child0ID() != nilNodeID || stale.child1ID() != nilNodeID {
		t.Fatal("retained backing still points at prior-function nodes")
	}
}

func TestScratchClearNodeReferencesArm64(t *testing.T) {
	e := &elem{}
	sc := scratch{}
	sc.fnState.regUser[0] = e
	sc.fnState.fregUser[0] = e
	sc.transient.tmpRoots = make([]*elem, 1, 4)
	sc.transient.tmpRoots[:cap(sc.transient.tmpRoots)][3] = e
	sc.transient.tmpDeferred = make([]deferredArg, 1, 4)
	sc.transient.tmpDeferred[:cap(sc.transient.tmpDeferred)][3].root = e

	sc.clearNodeReferences()
	if sc.fnState.regUser[0] != nil || sc.fnState.fregUser[0] != nil {
		t.Fatal("register-user table retained an operand node")
	}
	if sc.transient.tmpRoots[:cap(sc.transient.tmpRoots)][3] != nil ||
		sc.transient.tmpDeferred[:cap(sc.transient.tmpDeferred)][3].root != nil {
		t.Fatal("pointer-bearing scratch capacity retained an operand node")
	}
}

func TestScratchNodeResourceStatsArm64(t *testing.T) {
	nodes := int(shared.MaxRetainedStackArenaBytes/uint64(unsafe.Sizeof(elem{}))) + maxStackChunkCap
	sc := newScratchWithStackCap(minStackArenaCap)
	for i := 1; i < nodes; i++ {
		sc.stack.alloc()
	}
	peakCapacity := retainedStackArenaCapacityArm64(sc.stack)
	sc.finishStackFunction()
	retainedCapacity := retainedStackArenaCapacityArm64(sc.stack)

	elemBytes := uint64(unsafe.Sizeof(elem{}))
	ms := &ModuleStats{}
	ms.setNodeScratchStats(sc)
	if got, want := ms.Compile.NodeScratchReserved, uint64(minStackArenaCap)*elemBytes; got != want {
		t.Fatalf("initial node scratch = %d, want %d", got, want)
	}
	if got, want := ms.Compile.NodeScratchPeak, uint64(peakCapacity)*elemBytes; got != want {
		t.Fatalf("peak node scratch = %d, want %d", got, want)
	}
	if got, want := ms.Compile.NodeScratchRetained, uint64(retainedCapacity)*elemBytes; got != want {
		t.Fatalf("retained node scratch = %d, want %d", got, want)
	}
	if got, want := ms.Compile.NodeScratchDiscarded, uint64(peakCapacity-retainedCapacity)*elemBytes; got != want {
		t.Fatalf("discarded node scratch = %d, want %d", got, want)
	}
}

func retainedStackArenaCapacityArm64(s *stack) int {
	total := 0
	for i := range s.chunks {
		total += cap(s.chunks[i])
	}
	return total
}

func stackChunkCapsArm64(s *stack) []int {
	caps := make([]int, len(s.chunks))
	for i := range s.chunks {
		caps[i] = cap(s.chunks[i])
	}
	return caps
}

func TestStackArenaCapForBodyTinyFunctionArm64(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(0, 0))
	if cap(s.chunks[0]) != minStackArenaCap {
		t.Fatalf("tiny stack arena cap = %d, want %d", cap(s.chunks[0]), minStackArenaCap)
	}
}

func TestStackArenaCapForBodyMediumFunctionArm64(t *testing.T) {
	const bodyLen = 64
	const locals = 12
	want := bodyLen + locals/4 + 1
	s := newStackWithCap(stackArenaCapForBody(bodyLen, locals))
	if cap(s.chunks[0]) != want {
		t.Fatalf("medium stack arena cap = %d, want %d", cap(s.chunks[0]), want)
	}
}

func TestStackArenaCapForBodyLargeFunctionArm64(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(1024, 128))
	want := stackArenaCapForBody(1024, 128)
	if cap(s.chunks[0]) != want {
		t.Fatalf("large stack arena cap = %d, want %d", cap(s.chunks[0]), want)
	}
}

func TestStackArenaCapForHintsIgnoresLongImmediatesArm64(t *testing.T) {
	// A body with a few stack-producing opcodes and long immediates should reserve
	// from the opcode hint, not one arena elem per byte.
	const bodyLen = 64
	const nodes = 12
	want := nodes + nodes/2 + 1
	if got := stackArenaCapForHints(bodyLen, 0, nodes); got != want {
		t.Fatalf("stackArenaCapForHints(%d, 0, %d) = %d, want %d", bodyLen, nodes, got, want)
	}
}
