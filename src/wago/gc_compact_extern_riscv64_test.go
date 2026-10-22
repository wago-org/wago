//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type compactExternTestRoots struct {
	store     *referenceStore
	collector *gc.Collector
	words     []uint64
	scratch   gc.Root
}

type compactExternNativeTestRoots struct{ gcNativeFrameRoots }

func (r *compactExternNativeTestRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.rangeChain(fn, nil, nil)
}

func (r *compactExternNativeTestRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return r.rangeChain(nil, sink, nil)
}

func TestRV64CompactExternRootGeometry(t *testing.T) {
	flag := shared.GCFrameExternRefRootFlag
	function := shared.GCFrameFuncRefRootFlag
	for _, offsets := range [][]uint32{{flag | 8}, {0, flag | 8, function | 16}} {
		if !validGCFrameOffsetsKinds(offsets, 24, true, true) || validGCFrameOffsetsMode(offsets, 24, true) {
			t.Fatalf("extern root permission/geometry: %v", offsets)
		}
	}
	for _, offsets := range [][]uint32{{8, flag | 8}, {flag | 8, function | 8}, {flag | function | 8}, {flag | 24}} {
		if validGCFrameOffsetsKinds(offsets, 24, true, true) {
			t.Fatalf("ambiguous/out-of-range extern root admitted: %v", offsets)
		}
	}
}

func TestRV64CompactExternNativeFrameRoots(t *testing.T) {
	in, _ := ownedExceptionTestInstance(t, gc.ProfileThroughput, nil)
	state, err := in.compactExternCollector(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := in.gc.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	word, err := state.externFromAny(leaf, gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 16)
	var pin runtime.Pinner
	pin.Pin(&frame[0])
	defer pin.Unpin()
	binary.LittleEndian.PutUint64(frame[8:], word)
	roots := compactExternNativeTestRoots{gcNativeFrameRoots{owner: in, base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{shared.GCFrameExternRefRootFlag | 8}, frameBytes: 16, frameLayout: gcNativeFrameLayoutRISCV64, codeBase: 1, codeBytes: 1, allowExternalReturn: true}}
	if err := in.gc.CollectFull(&roots); err != nil {
		t.Fatal(err)
	}
	if _, err := in.gc.ObjectType(leaf); err != nil || binary.LittleEndian.Uint64(frame[8:]) != word {
		t.Fatalf("extern frame lost its value or rewrote opaque word: %v", err)
	}
	binary.LittleEndian.PutUint64(frame[8:], 0)
	if err := in.gc.CollectFull(&roots); err != nil {
		t.Fatal(err)
	}
	if len(state.shells) != 0 {
		t.Fatal("cleared extern frame retained its shell")
	}
}

func (r *compactExternTestRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	for _, word := range r.words {
		collector, ref := compactExternWordRoot(r.store, word)
		if collector == r.collector {
			r.scratch = gc.Root(ref)
			if !fn(&r.scratch) {
				return
			}
		}
	}
	r.scratch = 0
}

func (r *compactExternTestRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	for _, word := range r.words {
		collector, ref := compactExternWordRoot(r.store, word)
		if collector == r.collector && !sink.VisitRootRef(ref) {
			return false
		}
	}
	return true
}

func TestRV64CompactForeignExternPinAndReclaim(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			token, err := in.refStore.issueExternref("foreign")
			if err != nil {
				t.Fatal(err)
			}
			state, err := in.compactExternCollector(nil)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := state.anyFromForeign(token, gc.EmptyRoots{})
			if err != nil || !ref.IsObj() || state.foreign[ref] != token {
				t.Fatalf("compact foreign conversion=%v, %v", ref, err)
			}
			if in.refStore.releaseExternref(token) {
				t.Fatal("live compact wrapper lost its extern pin")
			}
			for _, kind := range []gc.RefTestKind{gc.RefTestAny, gc.RefTestEq, gc.RefTestI31, gc.RefTestStruct, gc.RefTestArray, gc.RefTestNone, gc.RefTestDefined} {
				matched, err := (*gcRefTestTableState)(nil).refTest(in.gc, uint64(ref), gc.RefTestTarget{Kind: kind})
				if err != nil || matched != (kind == gc.RefTestAny) {
					t.Fatalf("private foreign wrapper classification %d: %v,%v", kind, matched, err)
				}
			}
			roots := gc.RefSliceRoots{ref}
			var rootSet gc.RootSet = roots
			if err := in.gc.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			if state.foreign[ref] != token || state.byToken[token] != ref {
				t.Fatal("rooted foreign wrapper lost its identity")
			}
			allocs := testing.AllocsPerRun(1000, func() {
				if cached, err := state.anyFromForeign(token, rootSet); err != nil || cached != ref {
					panic("cached foreign wrapper changed")
				}
			})
			if allocs != 0 {
				t.Fatalf("warm compact extern lookup allocations=%v", allocs)
			}
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			if len(state.foreign) != 0 || len(state.byToken) != 0 || !in.refStore.releaseExternref(token) {
				t.Fatal("unreachable foreign wrapper retained identity or extern pin")
			}
			if _, err := state.anyFromForeign(token, gc.EmptyRoots{}); err == nil {
				t.Fatal("released extern token produced a compact wrapper")
			}
			t.Logf("foreign wrapper: 16 native bytes, cached lookup %g Go allocations; weak sidecar %d bytes excluding map capacity", allocs, unsafe.Sizeof(*state))
		})
	}
}

func TestRV64CompactConvertedExternShellReclamation(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			state, err := in.compactExternCollector(nil)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := in.gc.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			values := []gc.Ref{leaf, gc.Ref(15)}
			roots := &compactExternTestRoots{store: in.refStore, collector: in.gc}
			for _, value := range values {
				word, err := state.externFromAny(value, roots)
				if err != nil {
					t.Fatal(err)
				}
				roots.words = append(roots.words, word)
				if !compactExternWordReserved(in.refStore, word) {
					t.Fatal("converted extern identity was not reserved")
				}
			}
			for i, word := range roots.words {
				if got, err := state.anyFromExtern(word, roots); err != nil || got != values[i] {
					t.Fatalf("conversion round trip=%v,%v want%v", got, err, values[i])
				}
			}
			if err := in.gc.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if _, err := in.gc.ObjectType(leaf); err != nil {
				t.Fatalf("extern word lost its collector value: %v", err)
			}
			if allocs := testing.AllocsPerRun(1000, func() {
				if word, err := state.externFromAny(leaf, roots); err != nil || word != roots.words[0] {
					panic("cached converted extern identity changed")
				}
			}); allocs != 0 {
				t.Fatalf("warm converted extern allocations=%g", allocs)
			}
			words := roots.words
			roots.words = nil
			if err := in.gc.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			if len(state.shells) != 0 || len(state.byValue) != 0 {
				t.Fatal("unreachable converted extern shells retained metadata")
			}
			for _, word := range words {
				if compactExternWordReserved(in.refStore, word) {
					t.Fatal("dead converted extern identity stayed reserved")
				}
			}
			for i := uint32(0); i < 128; i++ {
				word, err := state.externFromAny(gc.Ref(i*2+1), roots)
				if err != nil {
					t.Fatal(err)
				}
				roots.words = append(roots.words[:0], word)
				if err := in.gc.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				if len(state.shells) != 1 || len(state.byValue) != 1 {
					t.Fatalf("single live i31 retained %d shells/%d identities", len(state.shells), len(state.byValue))
				}
			}
			roots.words = nil
			if err := in.gc.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if len(state.shells) != 0 || len(state.byValue) != 0 {
				t.Fatal("cleared i31 conversion retained weak metadata")
			}
			t.Logf("converted extern: 24 native bytes/shell, weak identity maps, cached lookup0Goalloc")
		})
	}
}

type compactExternPersistentTestRoots struct{ owner *Instance }

func (r compactExternPersistentTestRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	rangeLocalCompactExternRoots(r.owner, fn, nil)
}
func (r compactExternPersistentTestRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return rangeLocalCompactExternRoots(r.owner, nil, sink)
}
func TestRV64CompactExternPersistentCells(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			state, err := in.compactExternCollector(nil)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := in.gc.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			word, err := state.externFromAny(leaf, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			if got, err := state.storePersistent(in, 1, word); err != nil || got != word {
				t.Fatal(got, err)
			}
			roots := compactExternPersistentTestRoots{in}
			if err := in.gc.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if _, err := in.gc.ObjectType(leaf); err != nil {
				t.Fatal("persistent extern lost its object", err)
			}
			if _, err := state.storePersistent(in, 1, ^uint64(0)); err == nil {
				t.Fatal("forged store was admitted")
			}
			if allocs := testing.AllocsPerRun(1000, func() {
				if _, err := state.storePersistent(in, 1, word); err != nil {
					panic(err)
				}
			}); allocs != 0 {
				t.Fatal("warm persistent extern allocations", allocs)
			}
			foreign, err := in.refStore.issueExternref("foreign")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := state.storePersistent(in, 2, foreign); err != nil {
				t.Fatal(err)
			}
			if in.refStore.releaseExternref(foreign) {
				t.Fatal("persistent foreign extern lost its pin")
			}
			if _, err := state.storePersistent(in, 2, 0); err != nil {
				t.Fatal(err)
			}
			state.drain()
			if !in.refStore.releaseExternref(foreign) {
				t.Fatal("cleared persistent extern kept its pin")
			}
			if _, err := state.storePersistent(in, 1, 0); err != nil {
				t.Fatal(err)
			}
			if err := in.gc.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if len(state.shells) != 0 || compactExternWordReserved(in.refStore, word) {
				t.Fatal("cleared persistent cell retained shell metadata")
			}
			releaseInstanceCompactExtern(in)
			if len(state.owners) != 0 {
				t.Fatal("physical close retained persistent cell metadata")
			}
		})
	}
}
