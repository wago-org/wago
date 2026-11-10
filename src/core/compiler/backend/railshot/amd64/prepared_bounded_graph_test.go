//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func boundedGraphTestModule(n int) *wasm.Module {
	m := &wasm.Module{Code: make([]wasm.Func, n)}
	for i := range m.Code {
		m.Code[i].BodyBytes = []byte{0x0b}
	}
	return m
}

func boundedGraphCandidates(n int) []uint64 {
	var bits []uint64
	for i := 0; i < n; i++ {
		bits = markDirectPrepared(bits, n, i)
	}
	return bits
}

func boundedGraphRelocs(n int, edges ...[2]int) [][]callReloc {
	relocs := make([][]callReloc, n)
	for _, edge := range edges {
		relocs[edge[0]] = append(relocs[edge[0]], callReloc{target: uint32(edge[1])})
	}
	return relocs
}

func TestResolveBoundedPreparedEntriesRejectsRecursiveSCC(t *testing.T) {
	m := boundedGraphTestModule(2)
	hints := []funcHints{{flags: hintHasCall}, {flags: hintHasCall}}
	got := resolveBoundedPreparedEntries(m, boundedGraphCandidates(2), hints, boundedGraphRelocs(2, [2]int{0, 1}, [2]int{1, 0}), nil)
	if directPreparedMarked(got, 0) || directPreparedMarked(got, 1) {
		t.Fatalf("recursive SCC admitted: %064b", got)
	}
}

func TestResolveBoundedPreparedEntriesAdmitsAcyclicCalls(t *testing.T) {
	m := boundedGraphTestModule(2)
	hints := []funcHints{{flags: hintHasCall}, {}}
	got := resolveBoundedPreparedEntries(m, boundedGraphCandidates(2), hints, boundedGraphRelocs(2, [2]int{0, 1}), nil)
	if !directPreparedMarked(got, 0) || !directPreparedMarked(got, 1) {
		t.Fatalf("acyclic graph rejected: %064b", got)
	}
}

func TestResolveBoundedPreparedEntriesRejectsCallRef(t *testing.T) {
	m := boundedGraphTestModule(1)
	hints := []funcHints{{flags: hintHasCall}}
	hints[0].markNonDirectCall()
	hints[0].markUnsupportedDynamicCall()
	got := resolveBoundedPreparedEntries(m, boundedGraphCandidates(1), hints, boundedGraphRelocs(1), []immutableTableHint{{local: true}})
	if directPreparedMarked(got, 0) {
		t.Fatal("unsupported dynamic call admitted")
	}
}

func TestResolveBoundedPreparedEntriesAdmitsImmutableTableTargets(t *testing.T) {
	m := boundedGraphTestModule(2)
	m.Tables = []wasm.Table{{}}
	m.Elements = []wasm.Elem{{
		Mode: wasm.ElemMode{Kind: wasm.ElemActive},
		Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{1}},
	}}
	hints := []funcHints{{flags: hintHasCall}, {}}
	hints[0].markNonDirectCall()
	got := resolveBoundedPreparedEntries(m, boundedGraphCandidates(2), hints, boundedGraphRelocs(2), []immutableTableHint{{local: true}})
	if !directPreparedMarked(got, 0) || !directPreparedMarked(got, 1) {
		t.Fatalf("immutable table graph rejected: %064b", got)
	}
}

func TestResolveBoundedPreparedEntriesRejectsMutableTable(t *testing.T) {
	m := boundedGraphTestModule(2)
	m.Tables = []wasm.Table{{}}
	hints := []funcHints{{flags: hintHasCall}, {}}
	hints[0].markNonDirectCall()
	got := resolveBoundedPreparedEntries(m, boundedGraphCandidates(2), hints, boundedGraphRelocs(2), []immutableTableHint{{}})
	if directPreparedMarked(got, 0) {
		t.Fatal("dynamic call through unproven table admitted")
	}
}

func TestResolveBoundedPreparedEntriesEnforcesDepthCap(t *testing.T) {
	const n = maxBoundedPreparedCallDepth + 2
	m := boundedGraphTestModule(n)
	hints := make([]funcHints, n)
	edges := make([][2]int, 0, n-1)
	for i := 0; i+1 < n; i++ {
		hints[i].flags.set(hintHasCall)
		edges = append(edges, [2]int{i, i + 1})
	}
	got := resolveBoundedPreparedEntries(m, boundedGraphCandidates(n), hints, boundedGraphRelocs(n, edges...), nil)
	if directPreparedMarked(got, 0) {
		t.Fatal("call chain beyond depth cap admitted")
	}
	if !directPreparedMarked(got, n-1) {
		t.Fatal("bounded leaf rejected")
	}
}

func benchmarkResolveBoundedPreparedEntriesTableFanout(b *testing.B, n int) {
	m := boundedGraphTestModule(n)
	m.Tables = []wasm.Table{{}}
	m.Elements = []wasm.Elem{{
		Mode: wasm.ElemMode{Kind: wasm.ElemActive},
		Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{wasm.FuncIdx(n - 1)}},
	}}
	hints := make([]funcHints, n)
	for i := 0; i+1 < n; i++ {
		hints[i].flags.set(hintHasCall)
		hints[i].markNonDirectCall()
	}
	tables := []immutableTableHint{{local: true}}
	candidates := boundedGraphCandidates(n)
	relocs := boundedGraphRelocs(n)
	b.ResetTimer()
	for range b.N {
		got := resolveBoundedPreparedEntries(m, candidates, hints, relocs, tables)
		if !directPreparedMarked(got, 0) || !directPreparedMarked(got, n-1) {
			b.Fatal("bounded immutable-table fanout rejected")
		}
	}
}

func BenchmarkResolveBoundedPreparedEntriesTableFanout(b *testing.B) {
	for _, n := range []int{1000, 2000, 4000} {
		b.Run(fmt.Sprintf("functions=%d", n), func(b *testing.B) {
			benchmarkResolveBoundedPreparedEntriesTableFanout(b, n)
		})
	}
}
