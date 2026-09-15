//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func boundedTestModule(n int) *wasm.Module {
	m := &wasm.Module{Code: make([]wasm.Func, n)}
	for i := range m.Code {
		m.Code[i].BodyBytes = []byte{0x0b}
	}
	return m
}

func boundedCandidates(n int) []uint64 {
	var bits []uint64
	for i := 0; i < n; i++ {
		bits = markDirectPrepared(bits, n, i)
	}
	return bits
}

func boundedRelocs(t *testing.T, edges ...[2]int) callRelocTable {
	t.Helper()
	n := 0
	for _, edge := range edges {
		n = max(n, max(edge[0], edge[1])+1)
	}
	byCaller := make([][]callReloc, n)
	for _, edge := range edges {
		byCaller[edge[0]] = append(byCaller[edge[0]], callReloc{target: uint32(edge[1])})
	}
	table := newCallRelocTable(n, len(edges))
	for i := range byCaller {
		if !table.appendFunction(i, byCaller[i]) {
			t.Fatalf("append relocation function %d", i)
		}
	}
	return table
}

func emptyBoundedRelocs(t *testing.T, n int) callRelocTable {
	t.Helper()
	table := newCallRelocTable(n, 0)
	for i := 0; i < n; i++ {
		if !table.appendFunction(i, nil) {
			t.Fatalf("append empty relocation function %d", i)
		}
	}
	return table
}

func TestResolveBoundedPreparedEntriesRejectsRecursiveSCC(t *testing.T) {
	m := boundedTestModule(2)
	hints := []funcHints{{flags: hintHasCall}, {flags: hintHasCall}}
	relocs := boundedRelocs(t, [2]int{0, 1}, [2]int{1, 0})
	got := resolveBoundedPreparedEntries(m, boundedCandidates(2), hints, relocs, immutableTableHint{})
	if directPreparedMarked(got, 0) || directPreparedMarked(got, 1) {
		t.Fatalf("recursive SCC admitted: %064b", got)
	}
}

func TestResolveBoundedPreparedEntriesAdmitsAcyclicCalls(t *testing.T) {
	m := boundedTestModule(2)
	hints := []funcHints{{flags: hintHasCall}, {}}
	relocs := boundedRelocs(t, [2]int{0, 1})
	got := resolveBoundedPreparedEntries(m, boundedCandidates(2), hints, relocs, immutableTableHint{})
	if !directPreparedMarked(got, 0) || !directPreparedMarked(got, 1) {
		t.Fatalf("acyclic graph rejected: %064b", got)
	}
}

func TestResolveBoundedPreparedEntriesRejectsUnsupportedDynamicCall(t *testing.T) {
	m := boundedTestModule(1)
	hints := []funcHints{{flags: hintHasCall | hintHasNonDirectCall}}
	hints[0].markUnsupportedDynamicCall()
	relocs := emptyBoundedRelocs(t, 1)
	got := resolveBoundedPreparedEntries(m, boundedCandidates(1), hints, relocs, immutableTableHint{local: true})
	if directPreparedMarked(got, 0) {
		t.Fatal("unsupported dynamic call admitted")
	}
}

func TestResolveBoundedPreparedEntriesAdmitsImmutableTableTargets(t *testing.T) {
	m := boundedTestModule(2)
	m.Tables = []wasm.Table{{}}
	m.Elements = []wasm.Elem{{
		Mode: wasm.ElemMode{Kind: wasm.ElemActive},
		Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{1}},
	}}
	hints := []funcHints{{flags: hintHasCall | hintHasNonDirectCall}, {}}
	relocs := emptyBoundedRelocs(t, 2)
	got := resolveBoundedPreparedEntries(m, boundedCandidates(2), hints, relocs, immutableTableHint{local: true})
	if !directPreparedMarked(got, 0) || !directPreparedMarked(got, 1) {
		t.Fatalf("immutable table graph rejected: %064b", got)
	}
}

func TestResolveBoundedPreparedEntriesEnforcesDepthCap(t *testing.T) {
	const n = maxBoundedPreparedCallDepth + 2
	m := boundedTestModule(n)
	hints := make([]funcHints, n)
	edges := make([][2]int, 0, n-1)
	for i := 0; i+1 < n; i++ {
		hints[i].flags.set(hintHasCall)
		edges = append(edges, [2]int{i, i + 1})
	}
	got := resolveBoundedPreparedEntries(m, boundedCandidates(n), hints, boundedRelocs(t, edges...), immutableTableHint{})
	if directPreparedMarked(got, 0) {
		t.Fatal("call chain beyond depth cap admitted")
	}
	if !directPreparedMarked(got, n-1) {
		t.Fatal("bounded leaf rejected")
	}
}
