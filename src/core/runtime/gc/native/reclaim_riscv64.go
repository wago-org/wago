//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package gc

import (
	"sync"
	"sync/atomic"
)

// Reclaim observers track external ownership of managed objects. They must not
// allocate collector objects, change roots, or call back into the collector.
// A null reference reports Close and removes the observer.
var reclaimObserverCount atomic.Uint32
var reclaimObservers map[*Collector][]reclaimObserver
var reclaimObserverMu sync.RWMutex
var reclaimObserverSequence uint64 // protected by reclaimObserverMu; zero is the setter's slot

type reclaimObserver struct {
	id uint64
	fn func(Ref)
}

func (c *Collector) SetReclaimObserver(observer func(Ref)) {
	c.setReclaimObserver(0, observer)
}

// AddReclaimObserver registers an independent observer and returns its removal
// function. Registration and removal require the collector's mutator lock, as
// does Close. Removal is idempotent and does not remove other observers.
func (c *Collector) AddReclaimObserver(observer func(Ref)) func() {
	if observer == nil {
		return func() {}
	}
	reclaimObserverMu.Lock()
	reclaimObserverSequence++
	id := reclaimObserverSequence
	if id == 0 {
		reclaimObserverMu.Unlock()
		panic("gc: reclaim observer identity exhausted")
	}
	reclaimObserverMu.Unlock()
	c.setReclaimObserver(id, observer)
	return func() { c.setReclaimObserver(id, nil) }
}

func (c *Collector) setReclaimObserver(id uint64, observer func(Ref)) {
	reclaimObserverMu.Lock()
	defer reclaimObserverMu.Unlock()
	old, loaded := reclaimObservers[c]
	// Notifications read immutable snapshots without holding a lock while they
	// call ownership code. Registration is cold; copying avoids a lock per free.
	next := make([]reclaimObserver, 0, len(old)+1)
	for _, entry := range old {
		if entry.id != id {
			next = append(next, entry)
		}
	}
	if observer != nil {
		next = append(next, reclaimObserver{id: id, fn: observer})
	}
	if len(next) == 0 {
		if loaded {
			delete(reclaimObservers, c)
			reclaimObserverCount.Add(^uint32(0))
		}
		return
	}
	if reclaimObservers == nil {
		reclaimObservers = make(map[*Collector][]reclaimObserver)
	}
	reclaimObservers[c] = next
	if !loaded {
		reclaimObserverCount.Add(1)
	}
}
func (c *Collector) notifyObjectReclaimed(ref Ref) {
	if reclaimObserverCount.Load() == 0 {
		return
	}
	reclaimObserverMu.RLock()
	observers := reclaimObservers[c]
	reclaimObserverMu.RUnlock()
	for _, entry := range observers {
		entry.fn(ref)
	}
}
func (c *Collector) notifyCollectorClosed() {
	c.closeObjectEdgeSource()
	reclaimObserverMu.Lock()
	observers, ok := reclaimObservers[c]
	if ok {
		delete(reclaimObservers, c)
		reclaimObserverCount.Add(^uint32(0))
	}
	reclaimObserverMu.Unlock()
	if ok {
		for _, entry := range observers {
			entry.fn(Null())
		}
	}
}
