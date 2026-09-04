package gc

import (
	"slices"
	"testing"
)

type limitingRootSink struct {
	refs  []Ref
	limit int
}

func (s *limitingRootSink) VisitRootRef(r Ref) bool {
	s.refs = append(s.refs, r)
	return s.limit == 0 || len(s.refs) < s.limit
}

type fallbackTestRoots []Root

func (r fallbackTestRoots) RangeRoots(fn func(RootSlot) bool) {
	for i := range r {
		if !fn(&r[i]) {
			return
		}
	}
}

type unknownTestRoots struct{ fallbackTestRoots }

type acceptingRootSink struct{}

func (acceptingRootSink) VisitRootRef(Ref) bool { return true }

func TestComposedDirectRootWalksRemainAllocationFree(t *testing.T) {
	direct := directTestRoots{I31New(1), I31New(2)}
	groups := RootGroups{{Roots: &direct}}
	var scratch ArrayInitializerRootScratch
	if !scratch.prepareUniform(&direct, I31New(3)) {
		t.Fatal("prepare uniform failed")
	}
	sink := acceptingRootSink{}
	if got := testing.AllocsPerRun(1000, func() { groups.RangeRootRefs(sink) }); got != 0 {
		t.Fatalf("root group direct walk allocations = %v", got)
	}
	if got := testing.AllocsPerRun(1000, func() { scratch.RangeRootRefs(sink) }); got != 0 {
		t.Fatalf("array scratch direct walk allocations = %v", got)
	}
}

func TestRootGroupsAndArrayScratchHonorSinkStop(t *testing.T) {
	direct := directTestRoots{I31New(1), I31New(2)}
	fallback := fallbackTestRoots{Root(I31New(3))}
	groups := RootGroups{
		{Class: RootNativeFrame, Roots: nil},
		{Class: RootNativeFrame, Roots: &direct},
		{Class: RootGlobal, Roots: fallback},
	}
	sink := &limitingRootSink{limit: 2}
	groups.RangeRootRefs(sink)
	if !slices.Equal(sink.refs, []Ref{I31New(1), I31New(2)}) {
		t.Fatalf("group sink stop visited %v", sink.refs)
	}

	var scratch ArrayInitializerRootScratch
	if !scratch.prepareUniform(&direct, I31New(9)) {
		t.Fatal("prepare uniform failed")
	}
	sink = &limitingRootSink{limit: 1}
	scratch.RangeRootRefs(sink)
	if !slices.Equal(sink.refs, []Ref{I31New(1)}) {
		t.Fatalf("uniform scratch ignored first-set stop: %v", sink.refs)
	}
	if scratch.prepareFixed(nil, nil) {
		t.Fatal("active array scratch accepted nested prepare")
	}
	scratch.clear()

	values := []Value{RefValue(I31New(4)), RefValue(I31New(5))}
	if !scratch.prepareFixed(nil, values) {
		t.Fatal("prepare fixed failed")
	}
	seen := 0
	scratch.RangeRoots(func(slot RootSlot) bool {
		seen++
		slot.SetRef(I31New(int32(10 + seen)))
		return true
	})
	if seen != 2 || values[0].Ref != I31New(11) || values[1].Ref != I31New(12) {
		t.Fatalf("fixed scratch rewrite: seen=%d values=%v", seen, values)
	}
	sink = &limitingRootSink{}
	scratch.RangeRootRefs(sink)
	if !slices.Equal(sink.refs, []Ref{I31New(11), I31New(12)}) {
		t.Fatalf("fixed scratch refs = %v", sink.refs)
	}
	scratch.clear()

	var nilScratch *ArrayInitializerRootScratch
	if nilScratch.prepareUniform(nil, Null()) || nilScratch.prepareFixed(nil, nil) {
		t.Fatal("nil array scratch accepted prepare")
	}
}

func TestMutableRootScratchAdaptersCoverReferenceSlotsOnly(t *testing.T) {
	fields := []FieldDesc{
		{Kind: StorageRefNull},
		{Kind: StorageI32},
		{Kind: StorageV128},
		{Kind: StorageRefNull},
	}
	values := []Value{
		RefValue(I31New(1)),
		I32Value(7),
		V128Value(8, 9),
		RefValue(I31New(2)),
	}
	first := Root(I31New(3))
	var scratch InitializerRootScratch
	if !scratch.prepare(Slots{&first}, values, fields) {
		t.Fatal("prepare initializer scratch failed")
	}
	seen := 0
	scratch.RangeRoots(func(slot RootSlot) bool {
		seen++
		slot.SetRef(I31New(int32(20 + seen)))
		return true
	})
	if seen != 3 || Ref(first) != I31New(21) || values[0].Ref != I31New(22) || values[3].Ref != I31New(23) {
		t.Fatalf("initializer roots: seen=%d first=%v values=%v", seen, Ref(first), values)
	}
	if scratch.prepare(nil, nil, nil) {
		t.Fatal("active initializer scratch accepted nested prepare")
	}
	scratch.clear()
	if scratch.active || scratch.first != nil || scratch.values != nil || scratch.fields != nil {
		t.Fatalf("initializer scratch retained state: %+v", scratch)
	}
	var nilScratch *InitializerRootScratch
	if nilScratch.prepare(nil, nil, nil) {
		t.Fatal("nil initializer scratch accepted prepare")
	}

	words := []uint64{uint64(I31New(4)), 5, 6, 7, uint64(I31New(8))}
	var wordScratch InitializerWordRootScratch
	if !wordScratch.prepare(nil, words, fields) {
		t.Fatal("prepare word scratch failed")
	}
	seen = 0
	wordScratch.RangeRoots(func(slot RootSlot) bool {
		seen++
		if got := slot.GetRef(); got != I31New(4) && got != I31New(8) {
			t.Fatalf("unexpected word root %v", got)
		}
		slot.SetRef(I31New(int32(30 + seen)))
		return true
	})
	if seen != 2 || Ref(uint32(words[0])) != I31New(31) || Ref(uint32(words[4])) != I31New(32) || words[2] != 6 || words[3] != 7 {
		t.Fatalf("word scratch rewrite: seen=%d words=%v", seen, words)
	}
	if wordScratch.prepare(nil, nil, nil) {
		t.Fatal("active word scratch accepted nested prepare")
	}
	wordScratch.clear()
	var nilWordScratch *InitializerWordRootScratch
	if nilWordScratch.prepare(nil, nil, nil) {
		t.Fatal("nil word scratch accepted prepare")
	}
}

func TestConcreteRootSlotsAndCompositionEdges(t *testing.T) {
	called := false
	EmptyRoots{}.RangeRoots(func(RootSlot) bool {
		called = true
		return false
	})
	if called {
		t.Fatal("empty roots invoked callback")
	}

	root := Root(I31New(1))
	if got := root.GetRef(); got != I31New(1) {
		t.Fatalf("root get = %v", got)
	}
	root.SetRef(I31New(2))
	if Ref(root) != I31New(2) {
		t.Fatalf("root set = %v", Ref(root))
	}

	var arrayScratch ArrayInitializerRootScratch
	if !arrayScratch.prepareUniform(nil, I31New(3)) {
		t.Fatal("prepare uniform failed")
	}
	arrayScratch.RangeRoots(func(slot RootSlot) bool {
		if slot.GetRef() != I31New(3) {
			t.Fatalf("uniform slot get = %v", slot.GetRef())
		}
		slot.SetRef(I31New(4))
		return true
	})
	if arrayScratch.uniform != I31New(4) {
		t.Fatalf("uniform slot set = %v", arrayScratch.uniform)
	}
	arrayScratch.clear()

	words := []uint64{uint64(I31New(5))}
	wordSlot := wordRootSlot{words: words, idx: 0}
	if wordSlot.GetRef() != I31New(5) {
		t.Fatalf("word slot get = %v", wordSlot.GetRef())
	}
	wordSlot.SetRef(I31New(6))
	if Ref(uint32(words[0])) != I31New(6) {
		t.Fatalf("word slot set = %v", words[0])
	}
	values := []Value{RefValue(I31New(7))}
	valueSlot := valueRootSlot{values: values, idx: 0}
	if valueSlot.GetRef() != I31New(7) {
		t.Fatalf("value slot get = %v", valueSlot.GetRef())
	}
	valueSlot.SetRef(I31New(8))
	if values[0].Ref != I31New(8) {
		t.Fatalf("value slot set = %v", values[0].Ref)
	}
	refs := RefSliceRoots{I31New(9)}
	sliceSlot := sliceRootSlot{slice: refs, idx: 0}
	if sliceSlot.GetRef() != I31New(9) {
		t.Fatalf("slice slot get = %v", sliceSlot.GetRef())
	}
	sliceSlot.SetRef(I31New(10))
	if refs[0] != I31New(10) {
		t.Fatalf("slice slot set = %v", refs[0])
	}
	if !slotIndexOK(0, 1) || slotIndexOK(^uint32(0), 1) {
		t.Fatal("slot index bounds check mismatch")
	}

	seen := 0
	valueRootSet{values: []Value{RefValue(I31New(1)), I32Value(2)}, fields: []FieldDesc{{Kind: StorageRefNull}, {Kind: StorageI32}}}.RangeRoots(func(RootSlot) bool {
		seen++
		return false
	})
	if seen != 1 {
		t.Fatalf("filtered value roots visited %d slots", seen)
	}
	seen = 0
	valueRootSet{values: []Value{RefValue(I31New(1)), RefValue(I31New(2))}, all: true}.RangeRoots(func(RootSlot) bool {
		seen++
		return true
	})
	if seen != 2 {
		t.Fatalf("all-value roots visited %d slots", seen)
	}

	first, second := Root(I31New(11)), Root(I31New(12))
	combinedRootSet{first: Slots{&first}, second: Slots{&second}}.RangeRoots(func(RootSlot) bool { return false })
	extraRootSet{roots: nil, extra: &second}.RangeRoots(func(slot RootSlot) bool {
		seen++
		return true
	})
}

func TestRootSetFallbacksComposeAndRewrite(t *testing.T) {
	one, two, three := Root(I31New(1)), Root(I31New(2)), Root(I31New(3))
	classified := ClassifiedRoots{Class: RootPublicToken, Roots: Slots{&one, &two}}
	seen := 0
	classified.RangeRoots(func(slot RootSlot) bool {
		seen++
		slot.SetRef(I31New(int32(40 + seen)))
		return seen < 2
	})
	if seen != 2 || Ref(one) != I31New(41) || Ref(two) != I31New(42) {
		t.Fatalf("classified mutable walk: seen=%d refs=%v/%v", seen, Ref(one), Ref(two))
	}
	(ClassifiedRoots{}).RangeRoots(func(RootSlot) bool {
		t.Fatal("nil classified roots invoked callback")
		return false
	})
	if !((ClassifiedRoots{}).RangeRootRefs(&limitingRootSink{})) {
		t.Fatal("nil classified roots reported a stop")
	}
	direct := directTestRoots{I31New(4), I31New(5)}
	directSink := &limitingRootSink{}
	if !(ClassifiedRoots{Class: RootGlobal, Roots: &direct}).RangeRootRefs(directSink) || len(directSink.refs) != 2 {
		t.Fatalf("classified direct refs = %v", directSink.refs)
	}
	fallbackSink := &limitingRootSink{limit: 1}
	if (ClassifiedRoots{Class: RootGlobal, Roots: fallbackTestRoots{Root(I31New(6)), Root(I31New(7))}}).RangeRootRefs(fallbackSink) || len(fallbackSink.refs) != 1 {
		t.Fatalf("classified fallback stop = %v", fallbackSink.refs)
	}

	combined := combineRootSets(Slots{&one}, Slots{&two})
	combined = withExtraRoot(combined, &three)
	var got []Ref
	if !rangeRootRefs(combined, func(r Ref) bool {
		got = append(got, r)
		return true
	}) {
		t.Fatal("composed root set missed fast path")
	}
	if !slices.Equal(got, []Ref{Ref(one), Ref(two), Ref(three)}) {
		t.Fatalf("composed refs = %v", got)
	}

	refs := RefSliceRoots{I31New(5), I31New(6)}
	refs.RangeRoots(func(slot RootSlot) bool {
		slot.SetRef(I31New(7))
		return true
	})
	if !slices.Equal([]Ref(refs), []Ref{I31New(7), I31New(7)}) {
		t.Fatalf("slice roots were not mutable: %v", refs)
	}
	got = nil
	if !rangeRootRefs(refs, func(r Ref) bool { got = append(got, r); return true }) || len(got) != 2 {
		t.Fatalf("slice direct refs = %v", got)
	}
	if !rangeRootRefs(EmptyRoots{}, func(Ref) bool { t.Fatal("empty roots visited"); return false }) {
		t.Fatal("empty roots missed fast path")
	}
	if rangeRootRefs(unknownTestRoots{}, func(Ref) bool { return true }) {
		t.Fatal("unknown root set unexpectedly claimed a fast path")
	}

	groups := RootGroups{{Roots: Slots{&one, &two}}, {Roots: Slots{&three}}}
	seen = 0
	groups.RangeRoots(func(RootSlot) bool {
		seen++
		return seen < 2
	})
	if seen != 2 {
		t.Fatalf("mutable root groups ignored stop: %d", seen)
	}
	fallbackSink = &limitingRootSink{limit: 1}
	if groups.RangeRootRefs(fallbackSink) || len(fallbackSink.refs) != 1 {
		t.Fatalf("immutable root groups ignored stop: %v", fallbackSink.refs)
	}
	if combineRootSets(nil, Slots{&one}) == nil || combineRootSets(Slots{&one}, nil) == nil || combineRootSets(nil, nil) != nil {
		t.Fatal("nil root-set composition mismatch")
	}
}
