//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// The root and domain claim are independent of both the host resource and its
// importers. A public token can outlive either physical storage owner.
type compactExternDomainToken struct {
	store    *referenceStore
	domain   *gcStoreDomain
	word     uint64
	slot     uint32
	released atomic.Bool
}

func (v *compactExternDomainToken) externrefPublicValue() any { return v.word }
func (v *compactExternDomainToken) nativeExternWord() uint64  { return v.word }
func (v *compactExternDomainToken) releaseExternrefRoot() {
	if !v.released.CompareAndSwap(false, true) {
		return
	}
	unlockNative := lockNativeExecutionForHostAccess()
	v.domain.mu.Lock()
	var cleanup resourceCleanup
	cleanup.run(func() {
		if err := v.domain.collector.SetGlobalSlot(v.slot, gc.Null()); err != nil {
			panic(gcHelperFailure(err))
		}
		if state, ok := compactExternCollectors.Load(v.domain.collector); ok && state.hostSlots != nil {
			state.hostSlots.publicFree = append(state.hostSlots.publicFree, v.slot)
		}
		if value, ok := managedFunctionGraphs.Load(v.store); ok {
			graph := value.(*managedFunctionGraph)
			graph.epoch.Add(1)
			graph.pendingCollection.Store(true)
		}
	})
	cleanup.run(v.domain.mu.Unlock)
	cleanup.run(unlockNative)
	cleanup.run(func() { v.store.releaseUnclaimedGCCollector(v.domain.collector) })
	cleanup.run(func() { drainManagedFunctionStoreReclaims(v.store) })
	cleanup.finish()
}

// The native-state and collector-domain locks protect the root publication.
func (s *compactExternCollector) issueDomainExternToken(domain *gcStoreDomain, word uint64) (uint64, error) {
	collector, ref := compactExternWordRoot(s.store, word)
	if domain == nil || collector != s.collector || domain.collector != collector || ref.IsNull() {
		return 0, fmt.Errorf("host extern result has no exact live collector identity")
	}
	s.store.mu.Lock()
	if s.store.gcDomains.domainForCollectorLocked(collector) != domain || domain.claims == ^uint32(0) {
		s.store.mu.Unlock()
		return 0, fmt.Errorf("host extern result has no live collector domain")
	}
	domain.claims++
	s.store.mu.Unlock()
	if s.hostSlots == nil {
		s.hostSlots = &compactExternHostSlotPool{}
	}
	var slot uint32
	var err error
	free := s.hostSlots.publicFree
	if len(free) != 0 {
		slot = free[len(free)-1]
		s.hostSlots.publicFree = free[:len(free)-1]
		err = collector.SetGlobalSlot(slot, ref)
	} else {
		slot, err = collector.NewCheckedClassifiedGlobalSlot(ref, gc.RootPublicToken)
	}
	if err != nil {
		// The resource's own claim keeps the domain alive while this lock is held.
		s.store.mu.Lock()
		domain.claims--
		s.store.mu.Unlock()
		return 0, err
	}
	value := &compactExternDomainToken{store: s.store, domain: domain, word: word, slot: slot}
	token, err := s.store.issueExternref(value)
	if err != nil {
		_ = collector.SetGlobalSlot(slot, gc.Null())
		s.hostSlots.publicFree = append(s.hostSlots.publicFree, slot)
		s.store.mu.Lock()
		domain.claims--
		s.store.mu.Unlock()
		return 0, err
	}
	return token, nil
}

func railshotGetCompactExternHostGlobal(global *Global) (Value, bool, error) {
	if global == nil || global.owner == nil {
		return Value{}, false, nil
	}
	row := compactExternHostGlobalRow(global.owner)
	if row == nil {
		return Value{}, false, nil
	}
	row.domain.mu.Lock()
	defer row.domain.mu.Unlock()
	global.owner.mu.Lock()
	defer global.owner.mu.Unlock()
	if global.owner.closed || row.logicalClosed.Load() || !railshotCompactExternHostGlobalMatches(global, row.store, row.domain.collector) {
		return Value{}, true, fmt.Errorf("host extern global owner is closed or changed")
	}
	word := binary.LittleEndian.Uint64(global.cell)
	if word == 0 {
		return Value{typ: ValExternRef}, true, nil
	}
	if _, valid := row.store.resolveExternref(word); valid {
		return Value{typ: ValExternRef, bits: word}, true, nil
	}
	state, ok := compactExternCollectors.Load(row.domain.collector)
	if !ok {
		return Value{}, true, fmt.Errorf("host extern global has no live root state")
	}
	token, err := state.issueDomainExternToken(row.domain, word)
	return Value{typ: ValExternRef, bits: token}, true, err
}
