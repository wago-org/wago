// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
)

// Host storage has its own authority. Its explicit Close, not the lifetime of
// an importing or publishing Instance, consumes the external handle root.
func registerConversionHost(store *referenceStore, storage []byte, table *tableOwner, global *globalOwner) {
	if store == nil || len(storage) < 8 {
		return
	}
	topology := store.ensureGCTopology()
	topology.Lock()
	defer topology.Unlock()
	store.mu.Lock()
	defer store.mu.Unlock()
	if topology.conversionContainers == nil {
		topology.conversionContainers = make(map[*byte]*gcConversionContainer)
	}
	if topology.conversionContainers[&storage[0]] != nil {
		panic("duplicate host storage authority")
	}
	topology.conversionContainers[&storage[0]] = &gcConversionContainer{store: store, storage: storage, hostTable: table, hostGlobal: global, cell: global != nil, slots: make(map[uint64]*gcConversionReceipt)}
}

func conversionContainerForStore(store *referenceStore, owner *Instance, storage []byte) (*gcConversionContainer, error) {
	if store == nil || len(storage) < 8 {
		return nil, fmt.Errorf("conversion storage unavailable")
	}
	if owner != nil {
		if owner.refStore != store {
			return nil, fmt.Errorf("conversion storage crosses Runtime")
		}
		return newGCConversionContainer(owner, storage)
	}
	store.mu.Lock()
	var c *gcConversionContainer
	if store.gcDomains != nil {
		c = store.gcDomains.conversionContainers[&storage[0]]
	}
	store.mu.Unlock()
	if c == nil || c.owner != nil || c.closed || !c.hostLive() || len(c.storage) != len(storage) {
		return nil, fmt.Errorf("unknown, closed or foreign host conversion storage")
	}
	return c, nil
}

func (c *gcConversionContainer) hostLive() bool {
	if c == nil || c.closed || c.store == nil || len(c.storage) < 8 {
		return false
	}
	c.store.mu.Lock()
	registered := c.store.gcDomains != nil && c.store.gcDomains.conversionContainers[&c.storage[0]] == c
	c.store.mu.Unlock()
	if !registered {
		return false
	}
	if o := c.hostTable; o != nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		return !o.closed && o.instance == nil && o.store == c.store && o.arena != nil && o.elementType == ValExternRef
	}
	if o := c.hostGlobal; o != nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		return !o.closed && o.instance == nil && o.store == c.store && o.arena != nil && o.typ == ValExternRef
	}
	return false
}

// Take the complete ordered set before native execution, including domains
// whose first receipt is about to be created. No lease upgrade under a partial set.
func lockConversionHostAccess(store *referenceStore) func() {
	if store == nil {
		return func() {}
	}
	topology := store.ensureGCTopology()
	topology.RLock()
	store.mu.Lock()
	var set gcInvocationDomainSet
	for d := topology.first; d != nil; d = d.next {
		set.add(d)
	}
	for _, e := range store.instances {
		if e != nil && !e.resourcesReleased {
			set.add(e.gcDomain)
		}
	}
	store.mu.Unlock()
	set.sort()
	view := gcInvocationDomainView{set: &set}
	view.lock()
	id := newInvocationID()
	view.claim(id)
	nativeExecutionMu.Lock()
	for i := 0; i < view.len(); i++ {
		view.at(i).mu.Lock()
	}
	done := false
	return func() {
		if done {
			return
		}
		done = true
		for i := view.len() - 1; i >= 0; i-- {
			view.at(i).mu.Unlock()
		}
		nativeExecutionMu.Unlock()
		view.release(id)
		view.unlock()
		topology.RUnlock()
		drainManagedFunctionStoreReclaims(store)
	}
}

func lockConversionHostTableClose(t *Table) func() {
	if t == nil || t.owner == nil || t.owner.instance != nil || t.owner.elementType != ValExternRef {
		return func() {}
	}
	return lockConversionHostAccess(t.owner.store)
}
func lockConversionHostGlobalClose(g *Global) func() {
	if g == nil || g.owner == nil || g.owner.instance != nil || g.owner.typ != ValExternRef {
		return func() {}
	}
	return lockConversionHostAccess(g.owner.store)
}
func releaseConversionHostStorage(store *referenceStore, storage []byte) {
	if store == nil || len(storage) < 8 {
		return
	}
	store.mu.Lock()
	var c *gcConversionContainer
	if store.gcDomains != nil {
		c = store.gcDomains.conversionContainers[&storage[0]]
	}
	store.mu.Unlock()
	if c != nil && c.owner == nil {
		c.release()
	}
}

// Authenticate public tokens against this store, including a live publisher's
// not-yet-published conversion. Internal opaque words are never public ingress.
func conversionHostIncoming(store *referenceStore, word uint64) (uint64, *gcExternConversionState, error) {
	if word == 0 {
		return 0, nil, nil
	}
	store.mu.Lock()
	var sources []*gcExternConversionState
	if store.gcDomains != nil {
		for _, identity := range store.gcDomains.conversions {
			if identity.publicExtern == word {
				store.mu.Unlock()
				return identity.word, nil, nil
			}
		}
	}
	for in, entry := range store.instances {
		if entry != nil && !entry.resourcesReleased {
			if s := in.existingGCExternConversionState(); s != nil {
				sources = append(sources, s)
			}
		}
	}
	store.mu.Unlock()
	for _, source := range sources {
		source.mu.Lock()
		if !source.closed && source.core != nil {
			for _, entry := range source.entries {
				if entry.kind == gcExternConversionData && entry.publicExternWord == word {
					source.mu.Unlock()
					return entry.externWord, source, nil
				}
			}
		}
		source.mu.Unlock()
	}
	if _, ok := store.resolveExternref(word); ok {
		return word, nil, nil
	}
	return 0, nil, fmt.Errorf("invalid or foreign externref token")
}

func setConversionHostGlobal(g *Global, value Value) error {
	o := g.owner
	unlock := lockConversionHostAccess(o.store)
	defer unlock()
	o.mu.Lock()
	valid := !o.closed && o.instance == nil && o.typ == ValExternRef && g.Type == o.typ && g.Mutable == o.mutable && o.mutable && value.typ == ValExternRef && len(g.cell) >= 8
	nullable := !o.hasValueType || o.valueType.Ref.Nullable
	o.mu.Unlock()
	if !valid || value.bits == 0 && !nullable {
		return fmt.Errorf("invalid host global metadata or value")
	}
	c, err := conversionContainerForStore(o.store, nil, g.cell)
	if err != nil {
		return err
	}
	if c.hostGlobal != o {
		return fmt.Errorf("host global physical authority mismatch")
	}
	word, source, err := conversionHostIncoming(o.store, value.bits)
	if err != nil {
		return err
	}
	tx, err := c.prepare(0, word, source)
	if err != nil {
		return err
	}
	return tx.commit(func() error { writeGlobalObject(g, ValExternRef, word); return nil })
}

func setConversionHostTable(t *Table, index uint64, value Value) error {
	o := t.owner
	unlock := lockConversionHostAccess(o.store)
	defer unlock()
	o.mu.Lock()
	valid := !o.closed && o.instance == nil && o.elementType == ValExternRef
	o.mu.Unlock()
	if !valid || t.closed || value.typ != ValExternRef || len(t.desc) < 8 {
		return fmt.Errorf("invalid host table metadata or value")
	}
	c, err := conversionContainerForStore(o.store, nil, t.desc)
	if err != nil {
		return err
	}
	if c.hostTable != o {
		return fmt.Errorf("host table physical authority mismatch")
	}
	word, source, err := conversionHostIncoming(o.store, value.bits)
	if err != nil {
		return err
	}
	tx, err := c.prepare(index, word, source)
	if err != nil {
		return err
	}
	return tx.commit(func() error { binary.LittleEndian.PutUint64(t.desc[8+index*8:], word); return nil })
}

func conversionHostPublicWord(store *referenceStore, word uint64) (uint64, bool) {
	if store == nil || word == 0 {
		return 0, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.gcDomains != nil {
		if id := store.gcDomains.conversions[word]; id != nil {
			return id.publicExtern, true
		}
	}
	return 0, false
}

func conversionHostRootIsStrong(c *gcConversionContainer) bool { return c.hostLive() }
