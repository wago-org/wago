package compiler

import "testing"

func TestFunctionArtifactCacheIsBoundedLRUAndSnapshots(t *testing.T) {
	first := testFunctionArtifact(t)
	second := testFunctionArtifact(t)
	second.Identity.Function++
	second.IdentityFingerprint = second.Identity.Fingerprint()
	third := testFunctionArtifact(t)
	third.Identity.Function += 2
	third.IdentityFingerprint = third.Identity.Fingerprint()
	firstEncoded, err := MarshalFunctionArtifact(first)
	if err != nil {
		t.Fatal(err)
	}
	secondEncoded, err := MarshalFunctionArtifact(second)
	if err != nil {
		t.Fatal(err)
	}
	thirdEncoded, err := MarshalFunctionArtifact(third)
	if err != nil {
		t.Fatal(err)
	}
	charges := []uint64{uint64(len(firstEncoded)) + functionCacheEntryCharge, uint64(len(secondEncoded)) + functionCacheEntryCharge, uint64(len(thirdEncoded)) + functionCacheEntryCharge}
	budget := max(charges[0]+charges[1], charges[0]+charges[2], charges[1]+charges[2])
	cache := NewFunctionArtifactCache(budget)
	if stored, err := cache.Put(first); err != nil || !stored {
		t.Fatalf("put first = %t, %v", stored, err)
	}
	first.Code[0] = 99
	got, ok, err := cache.Get(first.Identity)
	if err != nil || !ok || got.Code[0] != 1 {
		t.Fatalf("snapshotted get = %#v, %t, %v", got.Code, ok, err)
	}
	if stored, err := cache.Put(second); err != nil || !stored {
		t.Fatalf("put second = %t, %v", stored, err)
	}
	if _, ok, err := cache.Get(first.Identity); err != nil || !ok {
		t.Fatalf("refresh first = %t, %v", ok, err)
	}
	if stored, err := cache.Put(third); err != nil || !stored {
		t.Fatalf("put third = %t, %v", stored, err)
	}
	if _, ok, err := cache.Get(second.Identity); err != nil || ok {
		t.Fatalf("least-recent entry retained: %t, %v", ok, err)
	}
	if _, ok, err := cache.Get(first.Identity); err != nil || !ok {
		t.Fatalf("recent entry evicted: %t, %v", ok, err)
	}
	stats := cache.Stats()
	if stats.Entries != 2 || stats.ChargedBytes > stats.MaxBytes || stats.Evictions != 1 || stats.Hits != 3 || stats.Misses != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestFunctionArtifactCacheRejectsOversizedAndInvalidArtifacts(t *testing.T) {
	artifact := testFunctionArtifact(t)
	cache := NewFunctionArtifactCache(1)
	if stored, err := cache.Put(artifact); err != nil || stored {
		t.Fatalf("oversized put = %t, %v", stored, err)
	}
	artifact.IdentityFingerprint[0]++
	if _, err := cache.Put(artifact); err == nil {
		t.Fatal("invalid artifact entered cache")
	}
	if stats := cache.Stats(); stats.Entries != 0 || stats.ChargedBytes != 0 {
		t.Fatalf("disabled cache retained data: %#v", stats)
	}
}
