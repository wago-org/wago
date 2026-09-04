package wago

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"
)

func TestArtifactAggregateDecodedBudget(t *testing.T) {
	r := compiledReader{data: []byte{2, 0, 0, 2, 0, 0}, budget: newArtifactDecodeBudget(int64(3 * artifactCollectionCopies * unsafe.Sizeof(ValueTypeDescriptor{})))}
	if _, err := r.valueTypes("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.valueTypes("second"); err == nil || !strings.Contains(err.Error(), "decoded allocation limit") {
		t.Fatalf("aggregate error = %v", err)
	}
}

func TestArtifactBudgetOverflowAndNestedSignature(t *testing.T) {
	b := newArtifactDecodeBudget(128)
	if err := b.charge(^uint64(0), 1024); err == nil {
		t.Fatal("overflow accepted")
	}
	if b.remaining != 128 {
		t.Fatal("failed reservation consumed budget")
	}
	types := []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Params: make([]ValueTypeDescriptor, 16)}}
	r := compiledReader{data: []byte{1, 1, 0, 0, 0, 0}, budget: newArtifactDecodeBudget(int64(artifactCollectionCopies*unsafe.Sizeof(FuncSig{})) + 128)}
	if _, err := r.funcSigs(types); err == nil || !strings.Contains(err.Error(), "decoded allocation limit") {
		t.Fatalf("referenced signature budget = %v", err)
	}
}

func TestArtifactCollectionsChargeElementWidths(t *testing.T) {
	r := compiledReader{data: []byte{4, 0, 0, 0, 0, 1, 0, 0, 0}, budget: newArtifactDecodeBudget(256)}
	if ints, err := r.intSlice(); err != nil || len(ints) != 4 {
		t.Fatalf("small integer directory: %v, %v", ints, err)
	}
	if _, err := r.funcSigs(nil); err == nil || !strings.Contains(err.Error(), "decoded allocation limit") {
		t.Fatalf("larger signature exceeded remaining budget: %v", err)
	}
}

func TestArtifactDecodedBudgetBeforeMetadataRead(t *testing.T) {
	// A valid framing prefix suffices: rejection must precede payload reading.
	var data []byte
	data = append(data, []byte(wagoMagic)...)
	data = append(data, wagoVersion, compiledSectionCount, compiledSectionCode, 0, compiledSectionMetadata, 64)
	limits := DefaultArtifactLimits()
	limits.MaxDecodedBytes = 32
	_, image, _, err := readCompiledFrom(bytes.NewReader(data), limits)
	if image != nil {
		image.Close()
		t.Fatal("image retained on failure")
	}
	if err == nil || !strings.Contains(err.Error(), "decoded allocation limit") {
		t.Fatalf("error = %v", err)
	}
}
