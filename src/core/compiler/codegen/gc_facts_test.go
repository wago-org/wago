package codegen

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
	if f.Identity() != 7 || f.Nullability() != GCKnownNonNull || f.HeapClass() != GCHeapArray || f.Freshness() != GCFreshUnpublished || f.Generation() != GCGenerationYoung || !f.PointerFree() {
		t.Fatalf("fact round trip = %+v", f)
	}
	if length, ok := f.KnownArrayLength(); !ok || length != ^uint32(0) {
		t.Fatalf("array length = %d,%v", length, ok)
	}
	if joined := MergeGCRefFacts(f, f); joined.Freshness() != GCPublished {
		t.Fatalf("structured fresh join retained uniqueness: %+v", joined)
	}
	published := f.WithFreshness(GCPublished)
	if merged := MergeGCRefFacts(f, published); merged.Identity() != 7 || merged.Freshness() != GCPublished {
		t.Fatalf("same-identity merge = %+v", merged)
	}
	other := ExactGCRefFact(3, 8, GCHeapArray).WithFreshness(GCFreshUnpublished).WithKnownArrayLength(4)
	if merged := MergeGCRefFacts(f, other); merged.Identity() != 0 || merged.HeapClass() != GCHeapArray || merged.Freshness() != GCPublished {
		t.Fatalf("distinct merge = %+v", merged)
	}
}

func TestGCBarrierSelectionFailsClosed(t *testing.T) {
	parent := ExactGCRefFact(1, 1, GCHeapStruct).WithFreshness(GCFreshUnpublished)
	child := ExactGCRefFact(2, 2, GCHeapStruct)
	if got := SelectGCStoreBarrier(parent, child); got != GCBarrierSlowBarrier {
		t.Fatalf("fresh parent selected %v, want slow barrier", got)
	}
	if got := SelectGCStoreBarrier(parent, NewGCRefFact(GCKnownNull, GCHeapStruct)); got != GCBarrierNoBarrier {
		t.Fatalf("null child selected %v", got)
	}
	if got := SelectGCStoreBarrier(parent, NewGCRefFact(GCKnownNonNull, GCHeapI31)); got != GCBarrierNoBarrier {
		t.Fatalf("i31 child selected %v", got)
	}
	if got := SelectGCBulkBarrier(parent.WithPointerFree(true), true); got != GCBarrierNoBarrier {
		t.Fatalf("pointer-free bulk selected %v", got)
	}
	for _, state := range []GCBarrierState{GCBarrierYoungParent, GCBarrierKnownOldChild, GCBarrierExistingCard, GCBarrierCardMark, GCBarrierSlowBarrier} {
		if !state.NeedsBarrier() {
			t.Fatalf("reserved barrier state %v failed open", state)
		}
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
