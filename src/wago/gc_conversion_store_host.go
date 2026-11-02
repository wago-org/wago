//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
)

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
		if word == 0 {
			return publish()
		}
		if _, ok := store.resolveExternref(word); ok {
			return publish()
		}
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
	if word == 0 {
		for i, cell := range owner.globalCells {
			if len(cell.cell) > 0 && &cell.cell[0] == &g.cell[0] {
				exact, e := owner.c.globalExactType(i)
				if e != nil {
					return true, e
				}
				if !exact.Ref.Nullable {
					return true, fmt.Errorf("global requires a non-null reference value")
				}
				break
			}
		}
	}
	if word != 0 {
		if internal, ok := conversionBoundaryWord(owner, word, true); ok {
			word = internal
		} else {
			internal, handled, e := owner.internalExternForGlobal(word)
			if e != nil {
				return true, fmt.Errorf("invalid externref token: %w", e)
			}
			if handled {
				word = internal
			} else if _, ok := owner.refStore.resolveExternref(word); !ok {
				return true, fmt.Errorf("invalid externref token")
			}
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
	tx, err := container.prepare(0, word, owner.existingGCExternConversionState())
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
	if owner == nil {
		for _, word := range words {
			if word != 0 {
				if _, ok := store.resolveExternref(word); !ok {
					return fmt.Errorf("converted host container has no physical owner")
				}
			}
		}
		publish()
		return nil
	}
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

const conversionInitializationEnabled = true

// Imported active extern segments are evaluated in private staging storage.
// Construction has released its old partial leases before this complete lease.
func (in *Instance) initializeConversionReceipts(staged []conversionInitialTable) error {
	if in.refStore == nil {
		return nil
	}
	source := in.existingGCExternConversionState()
	store := in.refStore
	store.mu.Lock()
	needed := store.gcDomains != nil && (len(store.gcDomains.conversions) != 0 || len(store.gcDomains.conversionContainers) != 0) || source != nil && source.core != nil
	store.mu.Unlock()
	if !needed && len(staged) == 0 {
		return nil
	}
	lease := in.lockGCInvocation(newInvocationID())
	defer func() { lease.unlock(); drainManagedFunctionStoreReclaims(store) }()
	unlock := in.lockInstanceNativeStateForHostAccess()
	defer unlock()
	for i := 0; i < lease.domains.len(); i++ {
		lease.domains.at(i).mu.Lock()
	}
	defer func() {
		for i := lease.domains.len() - 1; i >= 0; i-- {
			lease.domains.at(i).mu.Unlock()
		}
	}()
	transactions := []*gcConversionTransaction{}
	defer func() {
		for _, tx := range transactions {
			tx.rollback()
		}
	}()
	prepare := func(owner *Instance, storage []byte, index, word uint64) error {
		if word == 0 {
			store.mu.Lock()
			c := store.gcDomains.conversionContainers[&storage[0]]
			store.mu.Unlock()
			if c == nil || c.slots[index] == nil {
				return nil
			}
		}
		if owner == nil {
			if word == 0 {
				return nil
			}
			if _, ok := store.resolveExternref(word); ok {
				return nil
			}
		}
		c, e := newGCConversionContainer(owner, storage)
		if e != nil {
			return e
		}
		tx, e := c.prepare(index, word, source)
		if e != nil {
			return e
		}
		transactions = append(transactions, tx)
		return nil
	}
	for i, g := range in.globalCells {
		if i < len(in.c.GlobalImports) || in.c.Globals[i].Type != ValExternRef {
			continue
		}
		if e := prepare(in, g.cell, 0, readGlobalObject(g, ValExternRef)); e != nil {
			return e
		}
	}
	for i := in.c.tableImportCount(); i < in.c.tableCount(); i++ {
		if in.c.tableElementType(i) != ValExternRef {
			continue
		}
		storage := in.tableDescriptor(i)
		n := binary.LittleEndian.Uint32(storage)
		for j := uint64(0); j < uint64(n); j++ {
			if e := prepare(in, storage, j, binary.LittleEndian.Uint64(storage[8+j*8:])); e != nil {
				return e
			}
		}
	}
	for _, row := range staged {
		n := binary.LittleEndian.Uint32(row.staged)
		for j := uint64(0); j < uint64(n); j++ {
			if !row.touched[j] {
				continue
			}
			if e := prepare(row.owner, row.original, j, binary.LittleEndian.Uint64(row.staged[8+j*8:])); e != nil {
				return e
			}
		}
	}
	for _, row := range staged {
		for j, touched := range row.touched {
			if touched {
				copy(row.original[8+j*8:16+j*8], row.staged[8+j*8:16+j*8])
			}
		}
	}
	for _, tx := range transactions {
		if e := tx.commit(func() error { return nil }); e != nil {
			panic(e)
		}
	}
	return nil
}

func setConversionTablePublic(t *Table, index uint64, value Value) error {
	if t == nil || t.owner == nil || value.typ != ValExternRef {
		return fmt.Errorf("invalid extern table value")
	}
	owner := t.instanceOwner()
	if owner == nil {
		return fmt.Errorf("extern table mutation requires an instance-owned table")
	}
	if err := owner.beginInvocation(); err != nil {
		return err
	}
	defer owner.endInvocation()
	lease := owner.lockGCInvocation(newInvocationID())
	defer func() { lease.unlock(); drainManagedFunctionStoreReclaims(owner.refStore) }()
	unlock := owner.lockInstanceNativeStateForHostAccess()
	defer unlock()
	t.owner.mu.Lock()
	valid := !t.owner.closed && t.owner.elementType == ValExternRef && t.owner.store == owner.refStore
	t.owner.mu.Unlock()
	if !valid || len(t.desc) < 8 || index >= uint64(binary.LittleEndian.Uint32(t.desc)) {
		return fmt.Errorf("extern table metadata or index is invalid")
	}
	word := value.bits
	if word != 0 {
		internal, handled, err := owner.internalExternForGlobal(word)
		if err != nil {
			return err
		}
		if handled {
			word = internal
		} else if _, ok := owner.refStore.resolveExternref(word); !ok {
			return fmt.Errorf("invalid externref token")
		}
	}
	for i := 0; i < lease.domains.len(); i++ {
		lease.domains.at(i).mu.Lock()
	}
	defer func() {
		for i := lease.domains.len() - 1; i >= 0; i-- {
			lease.domains.at(i).mu.Unlock()
		}
	}()
	c, err := newGCConversionContainer(owner, t.desc)
	if err != nil {
		return err
	}
	tx, err := c.prepare(index, word, owner.existingGCExternConversionState())
	if err != nil {
		return err
	}
	return tx.commit(func() error { binary.LittleEndian.PutUint64(t.desc[8+index*8:], word); return nil })
}
