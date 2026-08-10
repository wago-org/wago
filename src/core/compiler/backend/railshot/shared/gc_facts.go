package shared

// GCRefFact is the backend-neutral, bounded semantic fact carried for one
// compact WasmGC reference. The first word packs type/class, identity,
// nullability, freshness, generation, and pointer-free state. The second word
// stores a known array length as length+1 so every u32 length is representable.
//
// It deliberately contains no raw object address. Backends may pair it with a
// separate short-lived resolver certificate whose safepoint invalidation rules
// are stricter than these semantic facts.
type GCRefFact struct {
	bits     uint64
	arrayLen uint64
}

type GCNullability uint8

const (
	GCNullUnknown GCNullability = iota
	GCKnownNull
	GCKnownNonNull
)

type GCHeapClass uint8

const (
	GCHeapUnknown GCHeapClass = iota
	GCHeapAny
	GCHeapEq
	GCHeapI31
	GCHeapStruct
	GCHeapArray
	GCHeapFunc
	GCHeapExtern
)

type GCFreshness uint8

const (
	GCFreshUnknown GCFreshness = iota
	GCFreshUnpublished
	GCPublished
)

// GCBarrierState is selected after structured facts have reached the store.
// The first three states require no emitted barrier; the remaining states are
// discriminated by the native runtime fast/medium path, with SlowBarrier as the
// conservative compile-time fallback.
type GCBarrierState uint8

const (
	GCBarrierNoBarrier GCBarrierState = iota
	GCBarrierYoungParent
	GCBarrierKnownOldChild
	GCBarrierExistingCard
	GCBarrierCardMark
	GCBarrierSlowBarrier
)

func (s GCBarrierState) NeedsBarrier() bool { return s >= GCBarrierExistingCard }

func SelectGCStoreBarrier(parent, child GCRefFact) GCBarrierState {
	if child.Nullability() == GCKnownNull || child.HeapClass() == GCHeapI31 || parent.PointerFree() {
		return GCBarrierNoBarrier
	}
	if parent.Freshness() == GCFreshUnpublished && parent.Identity() != 0 && parent.Generation() == GCGenerationYoung {
		return GCBarrierYoungParent
	}
	if child.Generation() == GCGenerationOld {
		return GCBarrierKnownOldChild
	}
	return GCBarrierSlowBarrier
}

func SelectGCBulkBarrier(destination GCRefFact, storesReferences bool) GCBarrierState {
	if !storesReferences || destination.PointerFree() {
		return GCBarrierNoBarrier
	}
	if destination.Freshness() == GCFreshUnpublished && destination.Identity() != 0 && destination.Generation() == GCGenerationYoung {
		return GCBarrierYoungParent
	}
	return GCBarrierSlowBarrier
}

type GCGeneration uint8

const (
	GCGenerationUnknown GCGeneration = iota
	GCGenerationYoung
	GCGenerationOld
)

const (
	gcFactPayloadMask   = uint64(1<<32) - 1
	gcFactIdentityShift = 32
	gcFactIdentityMask  = uint64(1<<20) - 1
	gcFactNullShift     = 52
	gcFactFreshShift    = 54
	gcFactGenShift      = 56
	gcFactPointerFree   = uint64(1) << 58
	gcFactExact         = uint64(1) << 59
	gcFactHeapShift     = 60
)

// MaxGCRefFactIdentity is the largest bounded compiler identity retained in a
// fact. Backends must conservatively use identity zero after exhausting it.
const MaxGCRefFactIdentity = uint32(gcFactIdentityMask)

func NewGCRefFact(nullability GCNullability, heap GCHeapClass) GCRefFact {
	var f GCRefFact
	f.bits = uint64(nullability)<<gcFactNullShift | uint64(heap)<<gcFactHeapShift
	return f
}

func ExactGCRefFact(typeIndex, identity uint32, heap GCHeapClass) GCRefFact {
	f := NewGCRefFact(GCKnownNonNull, heap)
	f.bits |= gcFactExact | uint64(typeIndex)
	return f.WithIdentity(identity)
}

func GCRefFactFromPacked(bits, arrayLen uint64) GCRefFact {
	return GCRefFact{bits: bits, arrayLen: arrayLen}
}

func (f GCRefFact) Packed() (bits, arrayLen uint64) { return f.bits, f.arrayLen }

func (f GCRefFact) IsZero() bool { return f.bits == 0 && f.arrayLen == 0 }

func (f GCRefFact) Nullability() GCNullability {
	return GCNullability((f.bits >> gcFactNullShift) & 3)
}

func (f GCRefFact) HeapClass() GCHeapClass {
	return GCHeapClass((f.bits >> gcFactHeapShift) & 15)
}

func (f GCRefFact) ExactType() (uint32, bool) {
	return uint32(f.bits & gcFactPayloadMask), f.bits&gcFactExact != 0
}

func (f GCRefFact) Identity() uint32 {
	return uint32((f.bits >> gcFactIdentityShift) & gcFactIdentityMask)
}

func (f GCRefFact) Freshness() GCFreshness {
	return GCFreshness((f.bits >> gcFactFreshShift) & 3)
}

func (f GCRefFact) Generation() GCGeneration {
	return GCGeneration((f.bits >> gcFactGenShift) & 3)
}

func (f GCRefFact) PointerFree() bool { return f.bits&gcFactPointerFree != 0 }

func (f GCRefFact) KnownArrayLength() (uint32, bool) {
	if f.arrayLen == 0 {
		return 0, false
	}
	return uint32(f.arrayLen - 1), true
}

func (f GCRefFact) WithNullability(v GCNullability) GCRefFact {
	f.bits &^= uint64(3) << gcFactNullShift
	f.bits |= uint64(v) << gcFactNullShift
	return f
}

func (f GCRefFact) WithHeapClass(v GCHeapClass) GCRefFact {
	f.bits &^= uint64(15) << gcFactHeapShift
	f.bits |= uint64(v) << gcFactHeapShift
	return f
}

func (f GCRefFact) WithExactType(typeIndex uint32, heap GCHeapClass) GCRefFact {
	f.bits &^= gcFactPayloadMask | (uint64(15) << gcFactHeapShift)
	f.bits |= gcFactExact | uint64(typeIndex) | uint64(heap)<<gcFactHeapShift
	return f
}

func (f GCRefFact) WithoutExactType() GCRefFact {
	f.bits &^= gcFactExact | gcFactPayloadMask
	return f
}

func (f GCRefFact) WithIdentity(identity uint32) GCRefFact {
	f.bits &^= gcFactIdentityMask << gcFactIdentityShift
	if identity <= MaxGCRefFactIdentity {
		f.bits |= uint64(identity) << gcFactIdentityShift
	}
	return f
}

func (f GCRefFact) WithFreshness(v GCFreshness) GCRefFact {
	f.bits &^= uint64(3) << gcFactFreshShift
	f.bits |= uint64(v) << gcFactFreshShift
	return f
}

func (f GCRefFact) WithGeneration(v GCGeneration) GCRefFact {
	f.bits &^= uint64(3) << gcFactGenShift
	f.bits |= uint64(v) << gcFactGenShift
	return f
}

func (f GCRefFact) WithPointerFree(v bool) GCRefFact {
	if v {
		f.bits |= gcFactPointerFree
	} else {
		f.bits &^= gcFactPointerFree
	}
	return f
}

func (f GCRefFact) WithKnownArrayLength(length uint32) GCRefFact {
	f.arrayLen = uint64(length) + 1
	return f
}

func (f GCRefFact) WithoutKnownArrayLength() GCRefFact {
	f.arrayLen = 0
	return f
}

// MergeGCRefFacts intersects facts from two structured predecessors. Identity
// and exact type survive only when equal. Published dominates fresh for one
// retained identity; merging distinct identities loses freshness and generation.
func MergeGCRefFacts(a, b GCRefFact) GCRefFact {
	var out GCRefFact
	if a.Nullability() == b.Nullability() {
		out = out.WithNullability(a.Nullability())
	}

	aType, aExact := a.ExactType()
	bType, bExact := b.ExactType()
	aHeap, bHeap := a.HeapClass(), b.HeapClass()
	switch {
	case aExact && bExact && aType == bType:
		out = out.WithExactType(aType, aHeap)
	case aHeap != GCHeapUnknown && aHeap == bHeap:
		out = out.WithHeapClass(aHeap)
	}

	identity := a.Identity()
	if identity != 0 && identity == b.Identity() {
		out = out.WithIdentity(identity)
		freshA, freshB := a.Freshness(), b.Freshness()
		switch {
		case freshA == GCPublished || freshB == GCPublished:
			out = out.WithFreshness(GCPublished)
		case freshA == GCFreshUnpublished && freshB == GCFreshUnpublished:
			// A structured join is an alias boundary even when both paths carry
			// the same bounded identity.
			out = out.WithFreshness(GCPublished)
		case freshA == freshB:
			out = out.WithFreshness(freshA)
		}
		if a.Generation() == b.Generation() {
			out = out.WithGeneration(a.Generation())
		}
	} else if a.Freshness() == GCPublished || b.Freshness() == GCPublished ||
		(a.Freshness() == GCFreshUnpublished && b.Freshness() == GCFreshUnpublished) {
		out = out.WithFreshness(GCPublished)
	}

	out = out.WithPointerFree(a.PointerFree() && b.PointerFree())
	if al, ok := a.KnownArrayLength(); ok {
		if bl, bok := b.KnownArrayLength(); bok && al == bl {
			out = out.WithKnownArrayLength(al)
		}
	}
	return out
}
