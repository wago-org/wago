//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func instantiateStagedGCStructBasic(t testing.TB, cfg GCConfig) (*Compiled, *Instance) {
	t.Helper()
	data, err := hex.DecodeString(stagedGCStructBasicHex)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileStagedGCStruct(data)
	if err != nil {
		t.Fatal(err)
	}
	in, err := instantiateCore(compiled, InstantiateOptions{GC: cfg})
	if err != nil {
		_ = compiled.Close()
		t.Fatal(err)
	}
	return compiled, in
}

func issueStagedGCStructToken(t testing.TB, in *Instance) (GCRef, gcRefTokenEntry) {
	t.Helper()
	ref, err := in.gc.NewStructDefaultWithRoots(0, gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := in.referenceStoreForBoundary()
	if err != nil {
		t.Fatal(err)
	}
	_, exactResults, err := exactFuncSignatureView(in.c.Funcs[0], in.c.Types)
	if err != nil || len(exactResults) != 1 {
		t.Fatalf("new exact results = %v, %v", exactResults, err)
	}
	token, err := store.issueGCRef(in, ref, exactResults[0])
	if err != nil {
		t.Fatal(err)
	}
	exact, owner, slot, ok := store.gcRefExactType(token)
	if !ok || owner != in || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != 0 {
		t.Fatalf("issued exact GC token = %#v owner=%p slot=%d ok=%v", exact, owner, slot, ok)
	}
	entry, entryOK := store.gcByToken[token]
	if !entryOK || entry.ref != ref || entry.slot != slot || token>>32 == 0 || token == uint64(ref) {
		t.Fatalf("issued GC token entry = %#v token=%#x compact=%#x", entry, token, ref)
	}
	rooted, err := in.gc.CheckedGlobalSlot(slot)
	if err != nil || rooted != ref {
		t.Fatalf("public GC token root = %v, %v; want %v", rooted, err, ref)
	}
	return GCRef{token: token}, entry
}

func TestStagedGCStructPublicTokenBoundedReuseAndRejection(t *testing.T) {
	compiled, in := instantiateStagedGCStructBasic(t, GCConfig{})
	defer compiled.Close()
	defer in.Close()

	first, firstEntry := issueStagedGCStructToken(t, in)
	store := in.refStore
	if store == nil {
		t.Fatal("public GC token store was not installed")
	}
	ref, err := in.gc.NewStructDefaultWithRoots(0, gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	_, exactResults, _ := exactFuncSignatureView(in.c.Funcs[0], in.c.Types)
	if _, issueErr := store.issueGCRef(in, ref, exactResults[0]); issueErr == nil || !strings.Contains(issueErr.Error(), "one live token") {
		t.Fatalf("second live GC token error = %v", issueErr)
	}

	otherCompiled, other := instantiateStagedGCStructBasic(t, GCConfig{})
	defer otherCompiled.Close()
	defer other.Close()
	if err := other.ReleaseGCRef(first); err == nil || !strings.Contains(err.Error(), "no GC reference token store") {
		t.Fatalf("foreign-store release error = %v", err)
	}
	if err := in.ReleaseGCRef(first); err != nil {
		t.Fatalf("release first token: %v", err)
	}
	if err := in.ReleaseGCRef(first); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale token release error = %v", err)
	}

	second, secondEntry := issueStagedGCStructToken(t, in)
	if secondEntry.slot != firstEntry.slot {
		t.Fatalf("public GC root slot grew from %d to %d instead of reusing its bounded slot", firstEntry.slot, secondEntry.slot)
	}
	if second.token == first.token {
		t.Fatal("released GC token was reused without a generation/random identity change")
	}
	if err := in.ReleaseGCRef(second); err != nil {
		t.Fatal(err)
	}
}

func TestStagedGCStructPublicTokenBothCloseOrdersAndTinyRecovery(t *testing.T) {
	t.Run("token-before-producer", func(t *testing.T) {
		compiled, in := instantiateStagedGCStructBasic(t, GCConfig{})
		ref, _ := issueStagedGCStructToken(t, in)
		if err := in.ReleaseGCRef(ref); err != nil {
			t.Fatal(err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		if in.hasPhysicalResources() {
			t.Fatal("token-before-producer close retained physical resources")
		}
		_ = compiled.Close()
	})

	t.Run("producer-before-token", func(t *testing.T) {
		compiled, in := instantiateStagedGCStructBasic(t, GCConfig{})
		ref, entry := issueStagedGCStructToken(t, in)
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		if !in.hasPhysicalResources() {
			t.Fatal("producer close released collector while a public GC token remained live")
		}
		if rooted, err := in.gc.CheckedGlobalSlot(entry.slot); err != nil || rooted != entry.ref {
			t.Fatalf("root after producer close = %v, %v; want %v", rooted, err, entry.ref)
		}
		if err := in.ReleaseGCRef(ref); err != nil {
			t.Fatal(err)
		}
		if in.hasPhysicalResources() {
			t.Fatal("final token release did not release logically closed producer resources")
		}
		_ = compiled.Close()
	})

	t.Run("tiny-repeated-release", func(t *testing.T) {
		cfg := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 96, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}
		compiled, in := instantiateStagedGCStructBasic(t, cfg)
		defer compiled.Close()
		defer in.Close()
		var slot uint32
		for i := 0; i < 100; i++ {
			ref, entry := issueStagedGCStructToken(t, in)
			if i == 0 {
				slot = entry.slot
			} else if entry.slot != slot {
				t.Fatalf("iteration %d root slot = %d, want bounded reuse of %d", i, entry.slot, slot)
			}
			if err := in.ReleaseGCRef(ref); err != nil {
				t.Fatalf("iteration %d release: %v", i, err)
			}
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatalf("iteration %d collect: %v", i, err)
			}
		}
	})
}
