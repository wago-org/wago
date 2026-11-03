//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

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

// Publication uses the authenticated native root and its acquired domain view.
// Never acquire or upgrade invocation leases here.
func (in *Instance) publishConversionStoreInvocation(authority gcConversionInvocation, owner *Instance, storage []byte, index, word uint64, publish func() error) error {
	if in.refStore == nil {
		if owner == nil {
			return fmt.Errorf("host conversion storage has no current store")
		}
		return publish()
	}
	source := in.existingGCExternConversionState()
	store := in.refStore
	if owner != nil && !store.conversionDomainEverEnabled.Load() {
		return publish()
	}
	store.mu.Lock()
	topology := store.gcDomains
	needed := topology != nil && (len(topology.conversions) != 0 || len(topology.conversionContainers) != 0) || source != nil && source.core != nil
	store.mu.Unlock()
	if owner != nil && (!needed || ordinaryConversionReplacement(store, storage, index, []uint64{word})) {
		return publish()
	}
	domains, admitted := in.conversionInvocationDomains(authority)
	if !admitted {
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
	container, err := conversionContainerForStore(store, owner, storage)
	if err != nil {
		return err
	}
	if err := validateConversionInvocation(domains, container, index, word, source); err != nil {
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
	if g.owner != nil && g.owner.instance == nil && g.owner.store != nil {
		if access != nil {
			if access.refStore != g.owner.store {
				return true, fmt.Errorf("host global access crosses Runtime")
			}
			if err := access.beginInvocation(); err != nil {
				return true, err
			}
			defer access.endInvocation()
		}
		return true, setConversionHostGlobal(g, value)
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
	if err := validateConversionInvocation(lease.domains, container, 0, word, owner.existingGCExternConversionState()); err != nil {
		return true, err
	}
	tx, err := container.prepare(0, word, owner.existingGCExternConversionState())
	if err != nil {
		return true, err
	}
	return true, tx.commit(func() error { writeGlobalObject(g, ValExternRef, word); return nil })
}

func (in *Instance) publishConversionRangeInvocation(authority gcConversionInvocation, owner *Instance, storage []byte, start, limit uint64, words []uint64, publish func()) error {
	if in.refStore == nil {
		if owner == nil {
			return fmt.Errorf("host conversion storage has no current store")
		}
		publish()
		return nil
	}
	store := in.refStore
	source := in.existingGCExternConversionState()
	if owner != nil && !store.conversionDomainEverEnabled.Load() {
		publish()
		return nil
	}
	store.mu.Lock()
	needed := store.gcDomains != nil && (len(store.gcDomains.conversions) != 0 || len(store.gcDomains.conversionContainers) != 0) || source != nil && source.core != nil
	store.mu.Unlock()
	if owner != nil && (!needed || ordinaryConversionReplacement(store, storage, start, words)) {
		publish()
		return nil
	}
	domains, admitted := in.conversionInvocationDomains(authority)
	if !admitted {
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
	c, err := conversionContainerForStore(store, owner, storage)
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
		if e := validateConversionInvocation(domains, c, start+uint64(i), word, source); e != nil {
			return e
		}
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
		if owner == nil {
			if _, err := conversionContainerForStore(store, nil, storage); err != nil {
				return err
			}
		}
		if word == 0 {
			store.mu.Lock()
			c := store.gcDomains.conversionContainers[&storage[0]]
			store.mu.Unlock()
			if c == nil || c.slots[index] == nil {
				return nil
			}
		}
		c, e := conversionContainerForStore(store, owner, storage)
		if e != nil {
			return e
		}
		if e := validateConversionInvocation(lease.domains, c, index, word, source); e != nil {
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
		return setConversionHostTable(t, index, value)
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
	exact, hasExact := t.owner.valueType, t.owner.hasValueType
	t.owner.mu.Unlock()
	if !valid || len(t.desc) < 8 || index >= uint64(binary.LittleEndian.Uint32(t.desc)) {
		return fmt.Errorf("extern table metadata or index is invalid")
	}
	word := value.bits
	if hasExact && (exact.Kind != ValueTypeReference || exact.Ref.Heap.Defined || exact.Ref.Heap.Abstract != AbstractHeapExtern && exact.Ref.Heap.Abstract != AbstractHeapNoExtern || word == 0 && !exact.Ref.Nullable || word != 0 && exact.Ref.Heap.Abstract == AbstractHeapNoExtern) {
		return fmt.Errorf("extern table value has an incompatible exact structural type")
	}
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
	if err := validateConversionInvocation(lease.domains, c, index, word, owner.existingGCExternConversionState()); err != nil {
		return err
	}
	tx, err := c.prepare(index, word, owner.existingGCExternConversionState())
	if err != nil {
		return err
	}
	return tx.commit(func() error { binary.LittleEndian.PutUint64(t.desc[8+index*8:], word); return nil })
}

// Ordinary tokens cannot name opaque identities. This allows a collector-free
// scalar producer called from another Runtime to update ordinary storage even
// when an unrelated conversion publisher lives in its Runtime. Both outgoing
// and incoming slots must be ordinary: clearing a receipt is never bypassed.
func ordinaryConversionReplacement(store *referenceStore, storage []byte, start uint64, words []uint64) bool {
	ordinary := func(word uint64) bool {
		if word == 0 {
			return true
		}
		_, ok := store.resolveExternref(word)
		return ok
	}
	for i, word := range words {
		offset := 8 + (start+uint64(i))*8
		if len(storage) == 8 {
			offset = 0
		}
		if offset+8 > uint64(len(storage)) || !ordinary(word) || !ordinary(binary.LittleEndian.Uint64(storage[offset:])) {
			return false
		}
	}
	return true
}

// Root identity comes from the native entry loop, never from the executing
// callee's stale public ID or an arbitrary collector's current lease holder.
func (in *Instance) conversionInvocationDomains(a gcConversionInvocation) (gcInvocationDomainView, bool) {
	if a.root == nil || a.id == 0 || a.root.refStore != in.refStore {
		return gcInvocationDomainView{}, false
	}
	if snapshot := a.root.conversionInvocationSnapshot(a.id); snapshot != nil {
		return snapshot.domains, snapshot.domains.ownedBy(a.id)
	}
	domains := a.root.gcInvocationDomains()
	if a.root.executionFlags.Load()&executionFlagDynamicGCDomain != 0 && !domains.dynamic {
		return gcInvocationDomainView{}, false
	}
	return domains, domains.ownedBy(a.id)
}

// Both the displaced and incoming receipt must belong to the acquired view.
// Registration may grow after conversion-only admission; that grants no new
// heap authority to this invocation. Native serialization protects the slots.
func validateConversionInvocation(domains gcInvocationDomainView, c *gcConversionContainer, index, word uint64, source *gcExternConversionState) error {
	contains := func(d *gcStoreDomain) bool {
		for i := 0; i < domains.len(); i++ {
			if domains.at(i) == d {
				return true
			}
		}
		return false
	}
	if old := c.slots[index]; old != nil && !contains(old.identity.domain) {
		return fmt.Errorf("old conversion receipt domain was not admitted")
	}
	c.store.mu.Lock()
	identity := c.store.gcDomains.conversions[word]
	c.store.mu.Unlock()
	if identity != nil {
		if !contains(identity.domain) {
			return fmt.Errorf("new conversion receipt domain was not admitted")
		}
	} else if word != 0 && source != nil && source.core != nil && !contains(source.core.domain) {
		return fmt.Errorf("conversion publisher domain was not admitted")
	}
	return nil
}
