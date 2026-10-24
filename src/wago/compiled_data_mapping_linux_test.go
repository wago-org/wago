//go:build linux

package wago

import (
	"bytes"
	"testing"
)

func TestCompiledLargeActiveDataUsesOwnedMapping(t *testing.T) {
	const segments = minCompactActiveData
	source := bytes.Repeat([]byte{0x5a}, minMappedCompactData+segments)
	c := &Compiled{Data: make([]DataInit, segments)}
	for i := range c.Data {
		start := i * len(source) / segments
		end := (i + 1) * len(source) / segments
		c.Data[i] = DataInit{Offset: OffsetInit{Base: uint32(start)}, Bytes: source[start:end:end]}
	}
	snapshot := cloneCompiledExecutionMetadata(c)
	if snapshot.compactData == nil || snapshot.compactData.owner == nil {
		t.Fatal("large active data did not use an owned mapping")
	}
	clear(source)
	got := snapshot.activeDataAt(0).Bytes[:16]
	if !bytes.Equal(got, bytes.Repeat([]byte{0x5a}, 16)) {
		t.Fatalf("mapped active data = %x", got)
	}
	if err := snapshot.compactData.owner.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot.compactData.bytes = nil
	snapshot.compactData.owner = nil
	if snapshot.compactData.owner != nil || snapshot.compactData.bytes != nil {
		t.Fatal("mapped active data retained after close")
	}
}
