//go:build !wago_gc_compact_helpers && !wago_gc_compact_reads

package gc

import (
	"testing"
	"unsafe"
)

func TestCompactViewDefaultStateIsEmpty(t *testing.T) {
	if unsafe.Sizeof(compactViewState{}) != 0 {
		t.Fatal("compact view adds default state")
	}
	c := new(Collector)
	if c.CompactView() != nil {
		t.Fatal("default exposes compact view")
	}
	if _, e := BuildCompactInstanceView(c, nil); e == nil {
		t.Fatal("default admits compact instance")
	}
}
