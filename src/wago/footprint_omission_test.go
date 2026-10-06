package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestArtifactSizeRejectsOmittedMetadata(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1), benchImportedModule(3, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	// Read physical section lengths from the serialized artifact, independently
	// of both ArtifactSectionSizes and its metadata-category counter.
	pos := 6
	readSection := func(id byte) []byte {
		t.Helper()
		if pos >= len(blob) || blob[pos] != id {
			t.Fatalf("missing section %d", id)
		}
		pos++
		size, n := binary.Uvarint(blob[pos:])
		if n <= 0 {
			t.Fatal("bad section length")
		}
		pos += n
		if size > uint64(len(blob)-pos) {
			t.Fatal("section exceeds artifact")
		}
		data := blob[pos : pos+int(size)]
		pos += int(size)
		return data
	}
	code := readSection(1)
	metadata := readSection(2)
	if pos != len(blob) || !bytes.Equal(code, c.code) {
		t.Fatal("physical code section differs")
	}
	// The first two metadata vectors are native entry offsets. Read their
	// actual encoded span; do not derive it from the report under test.
	entryEnd := 0
	for vector := 0; vector < 2; vector++ {
		count, n := binary.Uvarint(metadata[entryEnd:])
		if n <= 0 {
			t.Fatal("bad entry count")
		}
		entryEnd += n
		if count > uint64(len(metadata)-entryEnd) {
			t.Fatal("entry count exceeds metadata")
		}
		for i := uint64(0); i < count; i++ {
			_, n := binary.Varint(metadata[entryEnd:])
			if n <= 0 {
				t.Fatal("bad entry offset")
			}
			entryEnd += n
		}
	}
	physical, codeBytes, metadataBytes := int64(len(blob)), int64(len(code)), int64(len(metadata))
	framing := physical - codeBytes - metadataBytes
	reconcile := func(s ArtifactSectionSizes) error {
		attributed := s.Entries + s.Imports + s.Types + s.Functions + s.ExportsAndNames + s.Globals + s.Tables + s.Elements + s.Data + s.Memories + s.Tags + s.Features + s.GC
		if got := s.Framing + s.Code + s.Metadata; got != physical {
			return fmt.Errorf("unattributed artifact bytes: %d", physical-got)
		}
		if attributed != metadataBytes {
			return fmt.Errorf("unattributed metadata bytes: %d", metadataBytes-attributed)
		}
		if s.Total != physical || s.Code != codeBytes || s.Metadata != metadataBytes || s.Framing != framing || s.Entries != int64(entryEnd) {
			return fmt.Errorf("wrong category boundary")
		}
		return nil
	}
	sizes, err := c.ArtifactSectionSizes()
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcile(sizes); err != nil {
		t.Fatal(err)
	}
	if sizes.Metadata <= 0 || sizes.Entries <= 0 || sizes.Framing <= 0 {
		t.Fatal("fixture lacks a nonzero category")
	}
	cases := []struct {
		name   string
		change func(*ArtifactSectionSizes)
		want   string
	}{
		{"metadata", func(s *ArtifactSectionSizes) { s.Metadata = 0 }, fmt.Sprintf("unattributed artifact bytes: %d", metadataBytes)},
		{"entries", func(s *ArtifactSectionSizes) { s.Entries = 0 }, fmt.Sprintf("unattributed metadata bytes: %d", entryEnd)},
		{"framing", func(s *ArtifactSectionSizes) { s.Framing = 0 }, fmt.Sprintf("unattributed artifact bytes: %d", framing)},
		{"reassigned-entries", func(s *ArtifactSectionSizes) { s.Imports += s.Entries; s.Entries = 0 }, "wrong category boundary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := sizes
			tc.change(&bad)
			if err := reconcile(bad); err == nil || err.Error() != tc.want {
				t.Fatalf("negative control: %v, want %s", err, tc.want)
			}
		})
	}
	// Only report copies changed. The same positive gate still accepts the
	// original measurement, and serialization retains the original bytes.
	if err := reconcile(sizes); err != nil {
		t.Fatal(err)
	}
	after, err := c.MarshalBinary()
	if err != nil || !bytes.Equal(blob, after) {
		t.Fatal("negative control changed artifact")
	}
}

func BenchmarkArtifactFootprintCount(b *testing.B) {
	for _, size := range []int{0, 64 << 10, 8 << 20} {
		b.Run(fmt.Sprintf("payload=%d", size), func(b *testing.B) {
			c := &Compiled{PassiveData: []PassiveDataInit{{Bytes: make([]byte, size)}}}
			defer c.Close()
			want, err := c.ArtifactSectionSizes()
			if err != nil {
				b.Fatal(err)
			}
			var got ArtifactSectionSizes
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err = c.ArtifactSectionSizes()
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got != want || got.Data < int64(size) {
				b.Fatal("footprint count changed")
			}
			b.ReportMetric(float64(got.Metadata), "metadata-B")
		})
	}
}
