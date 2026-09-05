//go:build linux && amd64

package amd64

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNewStackArenaDefaultCapacity(t *testing.T) {
	s := newStack()
	if cap(s.chunks[0]) != defaultStackArenaCap {
		t.Fatalf("first chunk cap = %d, want %d", cap(s.chunks[0]), defaultStackArenaCap)
	}
}

func TestNewStackWithCapSizesFirstChunk(t *testing.T) {
	for _, tc := range []struct {
		hint int
		want int
	}{
		{0, minStackArenaCap},
		{minStackArenaCap - 1, minStackArenaCap},
		{minStackArenaCap + 7, minStackArenaCap + 7},
		{defaultStackArenaCap + 1, defaultStackArenaCap + 1}, // no upper clamp: chunked arena grows freely
	} {
		s := newStackWithCap(tc.hint)
		if cap(s.chunks[0]) != tc.want {
			t.Fatalf("newStackWithCap(%d) first chunk cap = %d, want %d", tc.hint, cap(s.chunks[0]), tc.want)
		}
		if s.head == nil || s.head.next != s.head || s.head.prev != s.head {
			t.Fatalf("newStackWithCap(%d) did not initialize sentinel links", tc.hint)
		}
	}
}

func TestStackCustomSidecarIsLazyAndCleared(t *testing.T) {
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

func TestHintedStackGrowthMatchesLegacyRetention(t *testing.T) {
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
		t.Fatalf("hinted fallback chunks = %v, want second cap %d", stackChunkCaps(hinted), 768-firstCap)
	}
	if got, want := retainedStackArenaCapacity(hinted), retainedStackArenaCapacity(legacy); got != want {
		t.Fatalf("hinted retained capacity = %d, want legacy %d; hinted=%v legacy=%v", got, want, stackChunkCaps(hinted), stackChunkCaps(legacy))
	}
}

func TestSubDefaultHintPreservesGeometricGrowth(t *testing.T) {
	const nodes = 2_000
	s := newStackWithCap(101)
	for i := 1; i < nodes; i++ {
		s.alloc()
	}
	want := []int{101, 202, 404, 808, 1616}
	if got := stackChunkCaps(s); !slices.Equal(got, want) {
		t.Fatalf("sub-default growth = %v, want %v", got, want)
	}
}

func TestStackFinishFunctionRetainsBoundedReusableOverflow(t *testing.T) {
	const nodes = 2_000
	s := newStackWithCap(minStackArenaCap)
	for i := 1; i < nodes; i++ {
		s.alloc()
	}
	wantCapacity := retainedStackArenaCapacity(s)
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
	oldCapacity := retainedStackArenaCapacity(s)
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
	if stale := s.chunks[0][:cap(s.chunks[0])][1]; stale.prev != nil || stale.next != nil || stale.arg0 != nil || stale.arg1 != nil {
		t.Fatal("retained backing still points at prior-function nodes")
	}
}

func TestScratchClearNodeReferences(t *testing.T) {
	e := &elem{}
	sc := scratch{}
	sc.fnState.regUser[0] = e
	sc.fnState.fregUser[0] = e
	sc.transient.tmpRoots = make([]*elem, 1, 4)
	sc.transient.tmpRoots[:cap(sc.transient.tmpRoots)][3] = e
	sc.transient.tmpBelow = make([]*elem, 1, 4)
	sc.transient.tmpBelow[:cap(sc.transient.tmpBelow)][3] = e
	sc.transient.tmpDeferred = make([]deferredArg, 1, 4)
	sc.transient.tmpDeferred[:cap(sc.transient.tmpDeferred)][3].root = e

	sc.clearNodeReferences()
	if sc.fnState.regUser[0] != nil || sc.fnState.fregUser[0] != nil {
		t.Fatal("register-user table retained an operand node")
	}
	if sc.transient.tmpRoots[:cap(sc.transient.tmpRoots)][3] != nil ||
		sc.transient.tmpBelow[:cap(sc.transient.tmpBelow)][3] != nil ||
		sc.transient.tmpDeferred[:cap(sc.transient.tmpDeferred)][3].root != nil {
		t.Fatal("pointer-bearing scratch capacity retained an operand node")
	}
}

func TestScratchNodeResourceStats(t *testing.T) {
	nodes := int(shared.MaxRetainedStackArenaBytes/uint64(unsafe.Sizeof(elem{}))) + maxStackChunkCap
	sc := newScratchWithStackCap(minStackArenaCap)
	for i := 1; i < nodes; i++ {
		sc.stack.alloc()
	}
	peakCapacity := retainedStackArenaCapacity(sc.stack)
	sc.finishStackFunction()
	retainedCapacity := retainedStackArenaCapacity(sc.stack)

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

func retainedStackArenaCapacity(s *stack) int {
	total := 0
	for i := range s.chunks {
		total += cap(s.chunks[i])
	}
	return total
}

func stackChunkCaps(s *stack) []int {
	caps := make([]int, len(s.chunks))
	for i := range s.chunks {
		caps[i] = cap(s.chunks[i])
	}
	return caps
}

func TestStackArenaCapForBodyTinyFunction(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(0, 0))
	if cap(s.chunks[0]) != minStackArenaCap {
		t.Fatalf("tiny stack first chunk cap = %d, want %d", cap(s.chunks[0]), minStackArenaCap)
	}
}

func TestStackArenaCapForBodyMediumFunction(t *testing.T) {
	const bodyLen = 64
	const locals = 12
	want := bodyLen + locals/4 + 1
	s := newStackWithCap(stackArenaCapForBody(bodyLen, locals))
	if cap(s.chunks[0]) != want {
		t.Fatalf("medium stack first chunk cap = %d, want %d", cap(s.chunks[0]), want)
	}
}

func TestStackArenaCapForBodyIncludesLocalAllowance(t *testing.T) {
	const bodyLen, locals = 64, 12
	want := bodyLen - bodyLen/4 + locals/4 + 1
	if got := stackArenaCapForBody(bodyLen, locals); got != want {
		t.Fatalf("stackArenaCapForBody(%d, %d) = %d, want %d", bodyLen, locals, got, want)
	}
}

func TestStackArenaPointerStabilityAcrossChunks(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	var last *elem
	// Push far past the first chunk so the arena advances through several
	// geometrically-grown chunks. Every earlier *elem must stay valid.
	const total = 4 * minStackArenaCap
	for i := 2; i <= total; i++ {
		last = s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	if len(s.chunks) < 2 {
		t.Fatalf("expected the arena to advance past the first chunk, got %d chunk(s)", len(s.chunks))
	}
	if first.st.cval != 1 || last.st.cval != total {
		t.Fatalf("stack values changed across chunk growth: first=%d last=%d", first.st.cval, last.st.cval)
	}
	// Walk the whole physical list and confirm contiguous values 1..total.
	want := int64(1)
	for e := s.head.next; e != s.head; e = e.next {
		if e.st.cval != want {
			t.Fatalf("list value at position %d = %d, want %d", want, e.st.cval, want)
		}
		want++
	}
	if want != int64(total)+1 {
		t.Fatalf("walked %d nodes, want %d", want-1, total)
	}
}

func TestStackArenaReusesChunksAcrossReset(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	for i := 0; i < 4*minStackArenaCap; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	grown := len(s.chunks)
	if grown < 2 {
		t.Fatalf("expected chunk growth, got %d", grown)
	}
	s.reset()
	// After reset the sentinel is back and every chunk is retained for reuse.
	if s.cur != 0 || len(s.chunks[0]) != 1 || s.head.next != s.head {
		t.Fatalf("reset did not rewind: cur=%d len0=%d", s.cur, len(s.chunks[0]))
	}
	for i := 0; i < 4*minStackArenaCap; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	if len(s.chunks) != grown {
		t.Fatalf("reuse allocated new chunks: %d, want %d retained", len(s.chunks), grown)
	}
}

func TestRegMask(t *testing.T) {
	m := maskOf(RAX, R12, R15)
	for _, r := range []Reg{RAX, R12, R15} {
		if !m.has(r) {
			t.Fatalf("mask should contain %v", r)
		}
	}
	if m.has(RCX) {
		t.Fatal("mask should not contain RCX")
	}
	if m.count() != 3 {
		t.Fatalf("count = %d, want 3", m.count())
	}
	m = m.remove(R12)
	if m.has(R12) || m.count() != 2 {
		t.Fatal("remove failed")
	}
	if got, ok := m.union(maskOf(RCX)).firstIn([]Reg{RDX, RCX, RAX}); !ok || got != RCX {
		t.Fatalf("firstIn = %v,%v, want RCX,true", got, ok)
	}
}

func TestRegLayout(t *testing.T) {
	// Reserved scratch regs are the trailing pool entries and include the fixed
	// x86 roles (RAX/RDX/RCX) and the return registers.
	for _, r := range []Reg{RAX, RDX, RCX, R8} {
		if !isScratchGP(r) {
			t.Errorf("%v should be a scratch GP", r)
		}
	}
	for _, r := range []Reg{RDI, R12, R15} {
		if isScratchGP(r) {
			t.Errorf("%v should NOT be reserved scratch", r)
		}
	}
	// linMem (RBX) and stack ptr (RSP) are not allocatable. RBP IS (frameless).
	for _, r := range []Reg{RBX, RSP} {
		if gpAllocPos(r) != -1 {
			t.Errorf("%v must not be in the allocation pool", r)
		}
	}
	if gpAllocPos(RBP) == -1 {
		t.Errorf("RBP must be allocatable in the frameless backend")
	}
	if gpAllocPos(RDI) != 0 {
		t.Errorf("RDI should be first in the pool, got pos %d", gpAllocPos(RDI))
	}
}

func TestAssignPinnedLocalsUsesLocalDefs(t *testing.T) {
	f := &fn{
		nLocals:   3,
		localType: []machineType{mtI32, mtF64, mtI32},
		m:         &wasm.Module{},
		sc:        &scratch{},
	}
	f.assignPinnedLocals([]uint32{1, 10, 5}, nil, pinnedLocalRegs, baseFPPins, false, false)

	r, isFloat, ok := f.pinReg(1)
	if !ok || !isFloat || r != pinnedFLocalRegs[0] {
		t.Fatalf("float local pin = %v,%v,%v", r, isFloat, ok)
	}
	r, isFloat, ok = f.pinReg(2)
	if !ok || isFloat || r != pinnedLocalRegs[0] {
		t.Fatalf("hot int local pin = %v,%v,%v", r, isFloat, ok)
	}
	if f.locals[2].state != lsReg {
		t.Fatalf("initial local state = %v, want lsReg", f.locals[2].state)
	}
}

// TestStackValentBlock builds `local.get 0; local.get 1; i32.add` and a nested
// `(a+b)+c`, checking the deferred-tree navigation: the add's operands, and the
// block base = the deepest-left leaf.
func TestStackValentBlock(t *testing.T) {
	f := &fn{s: newStack()}
	a := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	b := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
	f.pushBinOp(opAdd, mtI32) // a + b
	add := f.s.back()
	if !add.isDeferred() || add.arg0 != a || add.arg1 != b {
		t.Fatalf("add node operands wrong: arg0=%p arg1=%p (want a=%p b=%p)", add.arg0, add.arg1, a, b)
	}
	if base := baseOfValentBlock(add); base != a {
		t.Fatalf("baseOfValentBlock(add) = %p, want a=%p", base, a)
	}
	// nest: (a+b) + c
	c := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 2})
	f.pushBinOp(opAdd, mtI32)
	outer := f.s.back()
	if outer.arg0 != add || outer.arg1 != c {
		t.Fatalf("outer operands wrong: arg0=%p arg1=%p (want add=%p c=%p)", outer.arg0, outer.arg1, add, c)
	}
	if base := baseOfValentBlock(outer); base != a {
		t.Fatalf("baseOfValentBlock(outer) = %p, want a=%p (deepest-left leaf)", base, a)
	}
}
