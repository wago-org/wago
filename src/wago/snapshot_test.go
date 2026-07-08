package wago

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestLoadSnapshotRejectsOversizedCountsBeforeAlloc(t *testing.T) {
	for _, tc := range []struct {
		name string
		blob []byte
		want string
	}{
		{
			name: "globals",
			blob: appendSnapshotUvarints(snapshotPrefix(snapshotVersion), 1<<62),
			want: "global count",
		},
		{
			name: "passive data lengths",
			blob: appendSnapshotUvarints(snapshotPrefix(snapshotVersion), 0, 1<<62),
			want: "passive data length count",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadSnapshot(tc.blob)
			if err == nil {
				t.Fatal("LoadSnapshot accepted malicious count")
			}
			if !strings.Contains(err.Error(), "invalid snapshot") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadSnapshot error = %v, want invalid snapshot mentioning %q", err, tc.want)
			}
		})
	}
}

func snapshotPrefix(version byte) []byte {
	b := []byte(snapshotMagic)
	b = append(b, version, byte(SnapshotInit))
	return appendSnapshotUvarints(b,
		0, // compiled module byte length
		0, // memory pages
		0, // stored memory byte length
	)
}

func appendSnapshotUvarints(b []byte, vals ...uint64) []byte {
	for _, v := range vals {
		b = binary.AppendUvarint(b, v)
	}
	return b
}
