package gc

import "testing"

func TestTypedRefEpochInvalidatesPriorTypeEvidence(t *testing.T) {
	c, err := NewCollector(Config{}, typedRefDescs(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := c.TypedReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	if value.word>>NativeTypedRefEpochShift != 1 {
		t.Fatal("initial reference has wrong epoch")
	}
	// Model a checked-ABI callback fixture changing a header. This is not a
	// guest operation. The boundary must invalidate the previous type proof.
	c.AdvanceNativeTypeEpoch()
	header := c.header(raw)
	header.TypeID = 1
	c.writeHeader(raw, header)
	target := RefTestTarget{Kind: RefTestDefined, Type: 0, Exact: true}
	match, err := c.RefTestTyped(value, target)
	if err != nil || match {
		t.Fatalf("expired type evidence bypassed current header: %v/%v", match, err)
	}
	current, err := c.TypedReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id, known := current.TypeIndex(); !known || id != 1 || current.word>>NativeTypedRefEpochShift != 2 {
		t.Fatal("fresh carrier does not describe current object/epoch")
	}
	if !value.Equal(current) || NormalizeNativeRefWord(value.NativeWord()) != uint64(raw) {
		t.Fatal("epoch changed raw object identity")
	}
}

func TestTypedRefEpochExhaustionDoesNotReuseEvidence(t *testing.T) {
	c, err := NewCollector(Config{}, typedRefDescs(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	c.nativeView.TypeRefEpoch = NativeTypedRefMaxEpoch
	value, err := c.TypedReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	if uint32(value.word>>NativeTypedRefEpochShift) != NativeTypedRefMaxEpoch {
		t.Fatal("last valid epoch did not fit carrier")
	}
	c.AdvanceNativeTypeEpoch()
	c.AdvanceNativeTypeEpoch()
	if c.nativeView.TypeRefEpoch != 0 {
		t.Fatal("exhausted epoch was reused")
	}
	current, err := c.TypedReference(raw)
	if err != nil || current.NativeWord() != uint64(raw) {
		t.Fatalf("disabled mint did not retain raw fallback: %x/%v", current.NativeWord(), err)
	}
	header := c.header(raw)
	header.TypeID = 1
	c.writeHeader(raw, header)
	match, err := c.RefTestTyped(value, RefTestTarget{Kind: RefTestDefined, Type: 0, Exact: true})
	if err != nil || match {
		t.Fatalf("exhausted epoch accepted old evidence: %v/%v", match, err)
	}
}

func TestNativeRefWordNormalization(t *testing.T) {
	raw := makeObjRef(1)
	for _, epoch := range []uint64{0, 1, uint64(NativeTypedRefMaxEpoch)} {
		word := uint64(raw) | uint64(65535)<<32 | NativeTypedRefKnown | epoch<<NativeTypedRefEpochShift
		if got := NormalizeNativeRefWord(word); got != uint64(raw) {
			t.Fatalf("epoch%d normalized to%x", epoch, got)
		}
	}
	for _, word := range []uint64{0, uint64(I31New(-1)), uint64(raw), 1<<63 | 0x1234, 1<<32 | 0x5678} {
		if got := NormalizeNativeRefWord(word); got != word {
			t.Fatalf("untyped/opaque word%x changed to%x", word, got)
		}
	}
	for _, malformed := range []uint64{NativeTypedRefKnown, NativeTypedRefKnown | uint64(I31New(3))} {
		if NormalizeNativeRefWord(malformed) != malformed {
			t.Fatal("malformed typed word became a compact value")
		}
	}
}
