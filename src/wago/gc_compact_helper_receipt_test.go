package wago

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestGCCompactHelperReceiptIsPrivate(t *testing.T) {
	var absent *Compiled
	if absent.gcCompactHelperReceipt() != 0 || (&Compiled{}).gcCompactHelperReceipt() != 0 {
		t.Fatal("absent receipt admitted")
	}
	c := &Compiled{}
	before, err := compiledMetadataBytes(c, false)
	if err != nil {
		t.Fatal(err)
	}
	c.gcCompactHelperVersion = 1
	c.gcCompactNativeVersion = 1
	after, err := compiledMetadataBytes(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.gcCompactNativeReceipt() != 1 {
		t.Fatal("missing native receipt")
	}
	if !bytes.Equal(before, after) {
		t.Fatal("compact capability leaked into metadata")
	}
	snapshot := cloneCompiledExecutionMetadata(c)
	if snapshot == c || snapshot.gcCompactHelperReceipt() != 1 || snapshot.gcCompactNativeReceipt() != 1 {
		t.Fatal("private snapshot lost receipt")
	}
	c.gcCompactHelperVersion = 0
	if snapshot.gcCompactHelperReceipt() != 1 || snapshot.gcCompactNativeReceipt() != 1 {
		t.Fatal("public view mutation affected snapshot")
	}
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(Compiled{}) != 808 {
		t.Fatalf("Compiled footprint=%d want808", unsafe.Sizeof(Compiled{}))
	}
}
