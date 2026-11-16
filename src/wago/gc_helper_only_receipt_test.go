package wago

import (
	"bytes"
	"testing"
)

func TestGCHelperOnlyReceiptIsNotArtifactMetadata(t *testing.T) {
	var absent *Compiled
	if absent.hasGCHelperOnlyReceipt() || (&Compiled{}).hasGCHelperOnlyReceipt() {
		t.Fatal("absent receipt admitted")
	}
	c := &Compiled{}
	before, err := compiledMetadataBytes(c, false)
	if err != nil {
		t.Fatal(err)
	}
	c.gcHelperOnlyReceipt = true
	after, err := compiledMetadataBytes(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("live helper-only receipt leaked into serialized metadata")
	}
}
