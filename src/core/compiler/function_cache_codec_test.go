package compiler

import (
	"bytes"
	"testing"
)

func TestFunctionArtifactCacheSnapshotRoundTrip(t *testing.T) {
	want := testFunctionArtifact(t)
	cache := NewFunctionArtifactCache(1 << 20)
	if stored, err := cache.Put(want); err != nil || !stored {
		t.Fatalf("cache put = %t, %v", stored, err)
	}
	var first bytes.Buffer
	written, err := cache.SnapshotTo(&first)
	if err != nil || written != int64(first.Len()) {
		t.Fatalf("snapshot bytes = %d/%d, err %v", written, first.Len(), err)
	}
	var second bytes.Buffer
	if _, err := cache.SnapshotTo(&second); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("unchanged cache snapshots are not deterministic")
	}

	restored := NewFunctionArtifactCache(1 << 20)
	read, err := restored.RestoreFrom(bytes.NewReader(first.Bytes()))
	if err != nil || read != int64(first.Len()) {
		t.Fatalf("restore bytes = %d/%d, err %v", read, first.Len(), err)
	}
	got, hit, err := restored.Get(want.Identity)
	if err != nil || !hit {
		t.Fatalf("restored get = %t, %v", hit, err)
	}
	wantBytes, err := MarshalFunctionArtifact(want)
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, err := MarshalFunctionArtifact(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wantBytes, gotBytes) {
		t.Fatal("restored artifact differs from stored snapshot")
	}
}

func TestFunctionArtifactCacheRestoreIsBoundedAndAtomic(t *testing.T) {
	artifact := testFunctionArtifact(t)
	source := NewFunctionArtifactCache(1 << 20)
	if stored, err := source.Put(artifact); err != nil || !stored {
		t.Fatalf("cache put = %t, %v", stored, err)
	}
	var snapshot bytes.Buffer
	if _, err := source.SnapshotTo(&snapshot); err != nil {
		t.Fatal(err)
	}

	target := NewFunctionArtifactCache(64)
	if _, err := target.RestoreFrom(bytes.NewReader(snapshot.Bytes())); err == nil {
		t.Fatal("oversized snapshot restored into bounded cache")
	}
	if stats := target.Stats(); stats.Entries != 0 || stats.ChargedBytes != 0 {
		t.Fatalf("failed restore mutated cache: %#v", stats)
	}

	trailing := append(append([]byte(nil), snapshot.Bytes()...), 0)
	target = NewFunctionArtifactCache(1 << 20)
	if _, err := target.RestoreFrom(bytes.NewReader(trailing)); err == nil {
		t.Fatal("snapshot with trailing data restored")
	}
	if stats := target.Stats(); stats.Entries != 0 || stats.ChargedBytes != 0 {
		t.Fatalf("malformed restore mutated cache: %#v", stats)
	}
}
