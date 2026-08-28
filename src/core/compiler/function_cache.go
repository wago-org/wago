package compiler

import (
	"fmt"
	"sync"
)

const functionCacheEntryCharge = 128

type FunctionCacheStats struct {
	Entries      uint32
	ChargedBytes uint64
	MaxBytes     uint64
	Hits         uint64
	Misses       uint64
	Evictions    uint64
}

type cachedFunctionArtifact struct {
	artifact FunctionArtifact
	charge   uint64
	used     uint64
}

// FunctionArtifactCache is a process-shared, bounded cache of canonical
// function artifacts. It stores deep snapshots, so caller mutation cannot
// corrupt a warm entry. MaxBytes charges canonical payload size plus a fixed
// map-entry budget.
type FunctionArtifactCache struct {
	mu        sync.Mutex
	maxBytes  uint64
	charged   uint64
	clock     uint64
	hits      uint64
	misses    uint64
	evictions uint64
	entries   map[[32]byte]cachedFunctionArtifact
}

func NewFunctionArtifactCache(maxBytes uint64) *FunctionArtifactCache {
	return &FunctionArtifactCache{maxBytes: maxBytes}
}

// Put snapshots artifact. It returns false without mutation when one entry
// cannot fit the configured budget. A zero budget disables storage.
func (c *FunctionArtifactCache) Put(artifact FunctionArtifact) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("compiler function cache: nil cache")
	}
	encoded, err := MarshalFunctionArtifact(artifact)
	if err != nil {
		return false, err
	}
	charge := uint64(len(encoded)) + functionCacheEntryCharge
	if c.maxBytes == 0 || charge > c.maxBytes {
		return false, nil
	}
	key := artifact.IdentityFingerprint
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceClock()
	if existing, ok := c.entries[key]; ok {
		c.charged -= existing.charge
		delete(c.entries, key)
	}
	for c.charged+charge > c.maxBytes {
		c.evictOldest()
	}
	if c.entries == nil {
		c.entries = make(map[[32]byte]cachedFunctionArtifact)
	}
	c.entries[key] = cachedFunctionArtifact{artifact: cloneFunctionArtifact(artifact), charge: charge, used: c.clock}
	c.charged += charge
	return true, nil
}

func (c *FunctionArtifactCache) Get(identity FunctionArtifactIdentity) (FunctionArtifact, bool, error) {
	if c == nil {
		return FunctionArtifact{}, false, fmt.Errorf("compiler function cache: nil cache")
	}
	key := identity.Fingerprint()
	c.mu.Lock()
	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		c.mu.Unlock()
		return FunctionArtifact{}, false, nil
	}
	c.advanceClock()
	entry.used = c.clock
	c.entries[key] = entry
	c.hits++
	artifact := cloneFunctionArtifact(entry.artifact)
	c.mu.Unlock()
	return artifact, true, nil
}

func (c *FunctionArtifactCache) Stats() FunctionCacheStats {
	if c == nil {
		return FunctionCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return FunctionCacheStats{Entries: uint32(len(c.entries)), ChargedBytes: c.charged, MaxBytes: c.maxBytes, Hits: c.hits, Misses: c.misses, Evictions: c.evictions}
}

func (c *FunctionArtifactCache) evictOldest() {
	var oldestKey [32]byte
	oldestUsed := ^uint64(0)
	found := false
	for key, entry := range c.entries {
		if !found || entry.used < oldestUsed {
			oldestKey, oldestUsed, found = key, entry.used, true
		}
	}
	if !found {
		return
	}
	entry := c.entries[oldestKey]
	c.charged -= entry.charge
	delete(c.entries, oldestKey)
	c.evictions++
}

func (c *FunctionArtifactCache) advanceClock() {
	if c.clock != ^uint64(0) {
		c.clock++
		return
	}
	// Preserve LRU order across wrap without an unbounded auxiliary structure.
	oldest := ^uint64(0)
	for _, entry := range c.entries {
		if entry.used < oldest {
			oldest = entry.used
		}
	}
	if oldest == ^uint64(0) {
		c.clock = 1
		return
	}
	maximum := uint64(0)
	for key, entry := range c.entries {
		entry.used -= oldest
		if entry.used > maximum {
			maximum = entry.used
		}
		c.entries[key] = entry
	}
	c.clock = maximum + 1
}
