//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package gc

import (
	"sync"
	"sync/atomic"
)

// ObjectEdgeSource supplies managed references held outside an object's Wasm
// payload. References must belong to this collector. During incremental marking,
// existing indexes must remain stable: removed edges become null tombstones.
// New or replaced edges require WriteBarrierObject before publication. Callbacks
// must not allocate collector objects, change roots, or reenter the collector.
type ObjectEdgeSource interface {
	ObjectEdgeCount(Ref) uint32
	ObjectEdgeAt(Ref, uint32) Ref
}

// ConditionalRootSource is an optional policy on ObjectEdgeSource. A false
// result moves the slot's reachability to a managed owner edge. Its contents
// remain immutable to collection, and must be cleared before the owner can be
// used without that edge. Minor collections conservatively retain all slots.
type ConditionalRootSource interface {
	PersistentRootIsStrong(SlotKind, uint32) bool
}

// ObjectEdgeMutationSource invalidates a runtime joint mark after a published
// pointer write. It must only update metadata and cannot reenter the collector.
type ObjectEdgeMutationSource interface {
	ObjectEdgesMutated(Ref)
	ObjectRootPublished(Ref)
}

var objectEdgeSourceCount atomic.Uint32
var objectEdgeSources map[*Collector]ObjectEdgeSource
var objectEdgeSourceMu sync.RWMutex

// SetObjectEdgeSource installs the collector's cold ownership sidecar. The
// caller holds the collector's mutator lock and must retain it until Close.
func (c *Collector) SetObjectEdgeSource(source ObjectEdgeSource) {
	objectEdgeSourceMu.Lock()
	defer objectEdgeSourceMu.Unlock()
	if source == nil {
		if _, loaded := objectEdgeSources[c]; loaded {
			delete(objectEdgeSources, c)
			objectEdgeSourceCount.Add(^uint32(0))
		}
		return
	}
	if objectEdgeSources == nil {
		objectEdgeSources = make(map[*Collector]ObjectEdgeSource)
	}
	if _, loaded := objectEdgeSources[c]; !loaded {
		objectEdgeSourceCount.Add(1)
	}
	objectEdgeSources[c] = source
}

func (c *Collector) objectEdgeSource() ObjectEdgeSource {
	if objectEdgeSourceCount.Load() == 0 {
		return nil
	}
	objectEdgeSourceMu.RLock()
	source := objectEdgeSources[c]
	objectEdgeSourceMu.RUnlock()
	return source
}

func (c *Collector) objectEdgeCount(ref Ref) uint32 {
	if source := c.objectEdgeSource(); source != nil {
		return source.ObjectEdgeCount(ref)
	}
	return 0
}

func (c *Collector) persistentRootIsStrong(kind SlotKind, index uint32) bool {
	if policy, ok := c.objectEdgeSource().(ConditionalRootSource); ok {
		return policy.PersistentRootIsStrong(kind, index)
	}
	return true
}

func (c *Collector) objectEdgesMutated(ref Ref) {
	if source, ok := c.objectEdgeSource().(ObjectEdgeMutationSource); ok {
		source.ObjectEdgesMutated(ref)
	}
}

func (c *Collector) objectRootPublished(ref Ref) {
	if source, ok := c.objectEdgeSource().(ObjectEdgeMutationSource); ok {
		source.ObjectRootPublished(ref)
	}
}

func (c *Collector) closeObjectEdgeSource() { c.SetObjectEdgeSource(nil) }
