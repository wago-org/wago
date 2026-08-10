package shared

import "testing"

func TestGCRefFactRoundTripAndMerge(t *testing.T) {
	f := ExactGCRefFact(^uint32(0), 7, GCHeapArray).
		WithFreshness(GCFreshUnpublished).
		WithGeneration(GCGenerationYoung).
		WithPointerFree(true).
		WithKnownArrayLength(^uint32(0))
	if typ, ok := f.ExactType(); !ok || typ != ^uint32(0) {
		t.Fatalf("exact type = %d,%v", typ, ok)
	}
	if f.Identity() != 7 || f.Nullability() != GCKnownNonNull || f.HeapClass() != GCHeapArray ||
		f.Freshness() != GCFreshUnpublished || f.Generation() != GCGenerationYoung || !f.PointerFree() {
		t.Fatalf("fact round trip = %+v", f)
	}
	if length, ok := f.KnownArrayLength(); !ok || length != ^uint32(0) {
		t.Fatalf("array length = %d,%v", length, ok)
	}

	joinedFresh := MergeGCRefFacts(f, f)
	if joinedFresh.Freshness() != GCPublished {
		t.Fatalf("structured fresh join retained uniqueness: %+v", joinedFresh)
	}

	published := f.WithFreshness(GCPublished)
	merged := MergeGCRefFacts(f, published)
	if typ, ok := merged.ExactType(); !ok || typ != ^uint32(0) || merged.Identity() != 7 || merged.Freshness() != GCPublished {
		t.Fatalf("same-identity merge = %+v", merged)
	}

	other := ExactGCRefFact(3, 8, GCHeapArray).WithFreshness(GCFreshUnpublished).WithKnownArrayLength(4)
	merged = MergeGCRefFacts(f, other)
	if _, exact := merged.ExactType(); exact || merged.Identity() != 0 || merged.HeapClass() != GCHeapArray || merged.Freshness() != GCPublished {
		t.Fatalf("distinct merge = %+v", merged)
	}
	if _, ok := merged.KnownArrayLength(); ok {
		t.Fatalf("different lengths survived merge: %+v", merged)
	}

	knownClass := ExactGCRefFact(11, 0, GCHeapStruct)
	unknownClass := ExactGCRefFact(11, 0, GCHeapUnknown)
	merged = MergeGCRefFacts(unknownClass, knownClass)
	if typ, exact := merged.ExactType(); !exact || typ != 11 || merged.HeapClass() != GCHeapStruct {
		t.Fatalf("same exact type lost available heap class: %+v", merged)
	}
	merged = MergeGCRefFacts(knownClass, ExactGCRefFact(11, 0, GCHeapArray))
	if typ, exact := merged.ExactType(); !exact || typ != 11 || merged.HeapClass() != GCHeapUnknown {
		t.Fatalf("inconsistent exact classes were not intersected: %+v", merged)
	}
}

func BenchmarkMergeGCRefFacts(b *testing.B) {
	left := ExactGCRefFact(7, 9, GCHeapArray).WithFreshness(GCFreshUnpublished).WithKnownArrayLength(64)
	right := left.WithGeneration(GCGenerationYoung)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = MergeGCRefFacts(left, right)
	}
}

func TestGCBarrierSelection(t *testing.T) {
	parent := ExactGCRefFact(1, 1, GCHeapStruct).WithFreshness(GCFreshUnpublished)
	child := ExactGCRefFact(2, 2, GCHeapStruct)
	if got := SelectGCStoreBarrier(parent, child); got != GCBarrierSlowBarrier {
		t.Fatalf("unknown-generation fresh parent selected %v, want slow barrier", got)
	}
	if got := SelectGCStoreBarrier(parent.WithGeneration(GCGenerationYoung), child); got != GCBarrierYoungParent {
		t.Fatalf("young unpublished parent selected %v", got)
	}
	if got := SelectGCStoreBarrier(parent, child.WithGeneration(GCGenerationOld)); got != GCBarrierKnownOldChild {
		t.Fatalf("known-old child selected %v", got)
	}
	nullChild := NewGCRefFact(GCKnownNull, GCHeapStruct)
	if got := SelectGCStoreBarrier(parent, nullChild); got != GCBarrierNoBarrier {
		t.Fatalf("null child selected %v", got)
	}
	i31 := NewGCRefFact(GCKnownNonNull, GCHeapI31)
	if got := SelectGCStoreBarrier(parent, i31); got != GCBarrierNoBarrier {
		t.Fatalf("i31 child selected %v", got)
	}
	if got := SelectGCBulkBarrier(parent.WithPointerFree(true), false); got != GCBarrierNoBarrier {
		t.Fatalf("pointer-free bulk selected %v", got)
	}
}
