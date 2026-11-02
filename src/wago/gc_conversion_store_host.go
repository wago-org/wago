//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import "fmt"

func compiledConversionStoreHooks(c *Compiled) bool {
	if c == nil {
		return false
	}
	for _, g := range c.Globals {
		if g.Type == ValExternRef {
			return true
		}
	}
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValExternRef {
			return true
		}
	}
	return false
}

// Invocation admission freezes topology and acquires the complete sorted domain
// set, even for a collector-free provider. Never acquire invocation leases here.
func (in *Instance) publishConversionStore(owner *Instance, storage []byte, index, word uint64, publish func() error) error {
	if in.refStore == nil {
		return publish()
	}
	source := in.existingGCExternConversionState()
	store := in.refStore
	store.mu.Lock()
	topology := store.gcDomains
	needed := topology != nil && (len(topology.conversions) != 0 || len(topology.conversionContainers) != 0) || source != nil && source.core != nil
	store.mu.Unlock()
	if !needed {
		return publish()
	}
	domains := in.gcInvocationDomains()
	if !domains.ownedBy(in.currentInvocationID()) {
		return fmt.Errorf("conversion store lacks pre-admitted invocation domains")
	}
	// Domain mutation locks are below invocation/native authority, as for the
	// ordinary GC helper. All domains are already leased; no new lease can wait.
	for i := 0; i < domains.len(); i++ {
		domains.at(i).mu.Lock()
	}
	defer func() {
		for i := domains.len() - 1; i >= 0; i-- {
			domains.at(i).mu.Unlock()
		}
	}()
	if owner == nil {
		return fmt.Errorf("converted host container has no physical owner")
	}
	container, err := newGCConversionContainer(owner, storage)
	if err != nil {
		return err
	}
	tx, err := container.prepare(index, word, source)
	if err != nil {
		return err
	}
	return tx.commit(publish)
}

func conversionImportedTableCount(s *gcRefTestTableState) int {
	if s != nil && s.Conversion != nil && s.Conversion.core != nil {
		return s.Conversion.core.owner.c.tableImportCount()
	}
	return 0
}

// Boundary tokens remain distinct from internal opaque words after publisher Close.
func conversionBoundaryWord(in *Instance, word uint64, incoming bool) (uint64, bool) {
	if in == nil || in.refStore == nil || word == 0 {
		return 0, false
	}
	store := in.refStore
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.gcDomains == nil {
		return 0, false
	}
	if !incoming {
		if identity := store.gcDomains.conversions[word]; identity != nil {
			return identity.publicExtern, true
		}
	} else {
		for _, identity := range store.gcDomains.conversions {
			if identity.publicExtern == word {
				return identity.word, true
			}
		}
	}
	return 0, false
}

func setConversionGlobalPublic(access *Instance, g *Global, value Value) (bool, error) {
	if g == nil || g.Type != ValExternRef {
		return false, nil
	}
	owner := access
	if g.owner != nil {
		owner = g.owner.instance
	}
	if owner == nil || owner.refStore == nil {
		return false, nil
	}
	if access == nil {
		access = owner
	}
	if err := access.beginInvocation(); err != nil {
		return true, err
	}
	defer access.endInvocation()
	lease := access.lockGCInvocation(newInvocationID())
	defer func() { lease.unlock(); drainManagedFunctionStoreReclaims(owner.refStore) }()
	unlock := access.lockInstanceNativeStateForHostAccess()
	defer unlock()
	if value.typ != ValExternRef || !g.Mutable {
		return true, fmt.Errorf("invalid extern global value or immutable global")
	}
	if g.owner != nil {
		g.owner.mu.Lock()
		valid := !g.owner.closed && g.owner.typ == g.Type && g.owner.mutable == g.Mutable && g.owner.store == owner.refStore
		g.owner.mu.Unlock()
		if !valid {
			return true, fmt.Errorf("global reference owner metadata is invalid")
		}
	}
	word := value.bits
	if word != 0 {
		if internal, ok := conversionBoundaryWord(owner, word, true); ok {
			word = internal
		} else if _, ok := owner.refStore.resolveExternref(word); !ok {
			return true, fmt.Errorf("invalid or foreign externref token")
		}
	}
	container, err := newGCConversionContainer(owner, g.cell)
	if err != nil {
		return true, err
	}
	for i := 0; i < lease.domains.len(); i++ {
		lease.domains.at(i).mu.Lock()
	}
	defer func() {
		for i := lease.domains.len() - 1; i >= 0; i-- {
			lease.domains.at(i).mu.Unlock()
		}
	}()
	tx, err := container.prepare(0, word, nil)
	if err != nil {
		return true, err
	}
	return true, tx.commit(func() error { writeGlobalObject(g, ValExternRef, word); return nil })
}

func (in *Instance) publishConversionRange(owner *Instance, storage []byte, start, limit uint64, words []uint64, publish func()) error {
	if len(words) == 0 {
		publish()
		return nil
	}
	if in.refStore == nil {
		publish()
		return nil
	}
	store := in.refStore
	source := in.existingGCExternConversionState()
	store.mu.Lock()
	needed := store.gcDomains != nil && (len(store.gcDomains.conversions) != 0 || len(store.gcDomains.conversionContainers) != 0) || source != nil && source.core != nil
	store.mu.Unlock()
	if !needed {
		publish()
		return nil
	}
	domains := in.gcInvocationDomains()
	if !domains.ownedBy(in.currentInvocationID()) {
		return fmt.Errorf("bulk store lacks complete invocation authority")
	}
	for i := 0; i < domains.len(); i++ {
		domains.at(i).mu.Lock()
	}
	defer func() {
		for i := domains.len() - 1; i >= 0; i-- {
			domains.at(i).mu.Unlock()
		}
	}()
	c, err := newGCConversionContainer(owner, storage)
	if err != nil {
		return err
	}
	transactions := make([]*gcConversionTransaction, 0, len(words))
	defer func() {
		for _, tx := range transactions {
			tx.rollback()
		}
	}()
	for i, word := range words {
		tx, e := c.prepareLimit(start+uint64(i), word, limit, source)
		if e != nil {
			return e
		}
		transactions = append(transactions, tx)
	}
	// All validation/acquisition precedes this infallible publication. No callback,
	// reentry, allocation or trapping operation occurs during visible writes.
	publish()
	for _, tx := range transactions {
		if e := tx.commit(func() error { return nil }); e != nil {
			panic(e)
		}
	}
	return nil
}
