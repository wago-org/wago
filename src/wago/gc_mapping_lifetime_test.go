//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"sync"
	"testing"
)

func TestGCMappingCacheBoundedAcrossRetiredDomains(t *testing.T) {
	requireCompleteCore3Backend(t)
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Keep an instance in another domain alive while the cache is replaced.
	liveStore := newReferenceStore(false)
	defer liveStore.closeRuntime()
	live, err := instantiateCore(c, InstantiateOptions{store: liveStore})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	original := live.gcTypeMap
	store := newReferenceStore(false)
	defer store.closeRuntime()
	var firstDomain uint64
	for i := 0; i < 128; i++ {
		in, err := instantiateCore(c, InstantiateOptions{store: store})
		if err != nil {
			t.Fatal(err)
		}
		domain := in.gcInvocationDomain()
		if i == 0 {
			firstDomain = domain.id
		}
		// A live same-domain instance must reuse the mapping.
		peer, err := instantiateCore(c, InstantiateOptions{store: store})
		if err != nil {
			t.Fatal(err)
		}
		if in.gcTypeMap != peer.gcTypeMap {
			t.Fatal("same-domain mapping not reused")
		}
		if err := peer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		domains := store.gcDomains.n
		codeRetained := store.gcDomains.codeRanges != nil || store.gcDomains.singleCodeOwner != nil
		store.mu.Unlock()
		if codeRetained {
			t.Fatalf("cycle %d: retired code owner retained", i)
		}
		if domains != 0 {
			t.Fatalf("cycle %d: %d domains retained", i, domains)
		}
		entry := c.loadCompileIndexes().gcTypeMapping
		if entry == nil || entry.domainID != domain.id {
			t.Fatal("cache does not contain only the latest mapping")
		}
		if i > 0 && c.cachedGCTypeMapping(firstDomain, len(c.Types)) != nil {
			t.Fatal("retired mapping retained")
		}
		if len(entry.mapping.localToDomain) != len(c.Types) || len(entry.mapping.domainToLocalSparse)+len(entry.mapping.domainToLocal) > len(c.Types) {
			t.Fatal("mapping storage exceeds module type count")
		}
	}
	if live.gcTypeMap != original {
		t.Fatal("cache replacement changed live mapping")
	}
	if out, err := live.Invoke("set", 42); err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("live domain after eviction: %v %v", out, err)
	}
	t.Logf("after 128 retirements: 0 retired domains, 1 cached mapping, %d local entries, %d reverse entries", len(original.localToDomain), len(original.domainToLocalSparse)+len(original.domainToLocal))
}

func TestGCMappingCacheConcurrentDomains(t *testing.T) {
	requireCompleteCore3Backend(t)
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			for i := 0; i < 16; i++ {
				in, err := instantiateCore(c, InstantiateOptions{store: store})
				if err != nil {
					t.Error(err)
					return
				}
				if out, err := in.Invoke("set", uint64(i)); err != nil || len(out) != 1 || out[0] != uint64(i) {
					t.Errorf("invoke %v %v", out, err)
				}
				if err := in.Close(); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
