package wago

import (
	"encoding/binary"
	"fmt"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// InstanceExport is a handle to another instance's exported function, used as an
// import value for cross-instance linking. Place it in an Imports map under the
// importing module's "module.name" key; Instantiate binds the producer's entry,
// linear-memory base, and instance context into the importer's dispatch table.
//
// The referenced instance must remain physically live for as long as any
// importing instance can execute it. Import attachment retains its code, memory,
// descriptor arena, and context even if the producer is logically closed. A
// same-runtime public funcref token uses the same physical-root principle:
// token issuance retains the producer's code, descriptor arena, and home context
// until the shared reference store releases that token root.
type InstanceExport struct {
	inst     *Instance
	localIdx int
	params   []ValType
	results  []ValType
}

// ExportedFunc returns a handle to this instance's exported function `name`,
// suitable as a cross-instance import value in another module's Imports. A
// re-exported InstanceExport resolves to the original producer handle, preserving
// its code/context ownership and close-order requirement. Host-import re-exports
// remain fail-closed because they do not have an InstanceExport owner.
func (in *Instance) ExportedFunc(name string) (*InstanceExport, error) {
	if in == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	if err := in.beginInvocation(); err != nil {
		return nil, err
	}
	defer in.endInvocation()
	gfi, ok := in.c.Exports[name]
	if !ok {
		return nil, fmt.Errorf("no exported function %q", name)
	}
	if gfi < 0 {
		return nil, fmt.Errorf("export %q function index %d out of range", name, gfi)
	}
	if gfi < in.c.NumImports {
		if gfi >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", name, gfi)
		}
		ex, ok := in.imports[in.c.Imports[gfi]].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("export %q is an imported function without an InstanceExport owner", name)
		}
		return ex, nil
	}
	li := gfi - in.c.NumImports
	if li < 0 || li >= len(in.c.Funcs) {
		return nil, fmt.Errorf("export %q function index %d out of range", name, gfi)
	}
	sig := in.c.Funcs[li]
	in.nativeControlShared = true
	return &InstanceExport{inst: in, localIdx: li, params: sig.Params, results: sig.Results}, nil
}

// Table is a typed handle to a shared runtime table descriptor. The public
// handle stays 64 bytes: its pointer-sized owner field names the storage owner,
// exact element type, and (for externref) compatible reference store without
// putting Go pointers in the mmap-backed entries themselves.
type Table struct {
	desc  []byte
	owner *tableOwner
	next  *Table // lazy instance-owned export-handle chain

	mu       sync.Mutex
	closed   bool
	retained map[*Instance]struct{}
}

type tableOwner struct {
	mu          sync.Mutex
	arena       *coreruntime.Arena
	store       *referenceStore
	instance    *Instance
	elementType ValType
	// declaredHasMax records whether the table's external Wasm type declares an
	// explicit maximum. The runtime descriptor's capacity field is only an
	// allocation reservation (a no-max table still gets a finite reserve), so
	// import limit-matching must consult this instead of the descriptor: a table
	// with no declared maximum cannot satisfy an import that requires one.
	declaredHasMax bool
	importers      int
	closed         bool
}

// NewTable creates a host-owned funcref table that modules can import and share
// (e.g. the testsuite's spectest.table). Its entries start empty (an indirect
// call to one traps as uninitialized) until a module populates them via an active
// element segment. maxSize is the table.grow capacity; zero means minSize.
func NewTable(minSize, maxSize uint32) (*Table, error) {
	return newHostTable(minSize, maxSize, ValFuncRef, nil)
}

// NewExternRefTable creates a runtime/store-owned externref table. The table's
// 8-byte entries may be shared only by instances created by this Runtime. The
// table itself keeps the reference store alive after Runtime.Close until every
// importer is closed and Table.Close releases the final owner root.
func (rt *Runtime) NewExternRefTable(minSize, maxSize uint32) (*Table, error) {
	if rt == nil || rt.refStore == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, fmt.Errorf("wago: NewExternRefTable on a closed runtime")
	}
	return newHostTable(minSize, maxSize, ValExternRef, rt.refStore)
}

func newHostTable(minSize, maxSize uint32, elementType ValType, store *referenceStore) (*Table, error) {
	if maxSize != 0 && maxSize < minSize {
		return nil, fmt.Errorf("wago: table maximum %d < minimum %d", maxSize, minSize)
	}
	if maxSize == 0 {
		maxSize = minSize
	}
	entryBytes := coreruntime.TableEntryBytes
	if elementType == ValExternRef {
		entryBytes = 8
	}
	need64 := uint64(8) + uint64(maxSize)*uint64(entryBytes)
	if need64 > uint64(maxInt()) {
		return nil, fmt.Errorf("wago: table storage %d bytes overflows int", need64)
	}
	arena, err := coreruntime.NewArena(int(need64))
	if err != nil {
		return nil, err
	}
	if store != nil {
		if err := store.registerStoreObject(); err != nil {
			_ = arena.Close()
			return nil, err
		}
	}
	desc := arena.Alloc(int(need64))
	binary.LittleEndian.PutUint32(desc, minSize)
	binary.LittleEndian.PutUint32(desc[4:], maxSize)
	// Host tables are always bounded: maxSize defaulted to minSize above, so the
	// reservation is the effective declared maximum.
	owner := &tableOwner{arena: arena, store: store, elementType: elementType, declaredHasMax: true}
	return &Table{desc: desc, owner: owner}, nil
}

// Size returns the table's current descriptor length. It reflects table.grow on
// host-created, imported, and re-exported tables.
func (t *Table) Size() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || len(t.desc) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(t.desc))
}

// Close releases a host-created table after every importer closes. Instance-owned
// export handles remain no-ops; their producer instance owns the descriptor.
func (t *Table) Close() error {
	if t == nil || t.owner == nil {
		return nil
	}
	o := t.owner
	o.mu.Lock()
	if o.closed || o.arena == nil {
		o.mu.Unlock()
		return nil
	}
	if o.importers != 0 {
		count := o.importers
		o.mu.Unlock()
		return fmt.Errorf("wago: table has %d live importer(s); close consumers before the table", count)
	}
	o.closed = true
	arena, store := o.arena, o.store
	o.arena = nil

	// Lock order is tableOwner.mu -> Table.mu. Readers that need both use the
	// same order; producer roots are released only after both locks are dropped,
	// because releasing a root may re-enter instance finalization.
	t.mu.Lock()
	t.closed = true
	t.desc = nil
	roots := make([]*Instance, 0, len(t.retained))
	for root := range t.retained {
		roots = append(roots, root)
	}
	t.retained = nil
	t.mu.Unlock()
	o.mu.Unlock()

	for _, root := range roots {
		root.releaseResourceRoot()
	}
	err := arena.Close()
	if store != nil {
		store.storeObjectClosed()
	}
	return err
}

func (t *Table) validateImport(elementType ValType, store *referenceStore) error {
	if t == nil || t.owner == nil {
		return fmt.Errorf("table descriptor is invalid")
	}
	o := t.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("table owner is closed")
	}
	t.mu.Lock()
	validStorage := !t.closed && len(t.desc) >= 8
	t.mu.Unlock()
	if !validStorage {
		return fmt.Errorf("table descriptor is invalid")
	}
	if o.instance != nil {
		if closed := o.instance.isLogicallyClosed(); closed {
			return fmt.Errorf("table owner instance is closed")
		}
	}
	if o.elementType != elementType {
		return fmt.Errorf("table element type %s is incompatible with required %s", o.elementType, elementType)
	}
	if elementType == ValExternRef {
		if store == nil {
			return fmt.Errorf("externref table requires an explicit compatible reference store")
		}
		if o.store == nil || o.store != store {
			return fmt.Errorf("externref table belongs to an incompatible reference store")
		}
	}
	return nil
}

func (t *Table) attachImporter(elementType ValType, store *referenceStore) error {
	if err := t.validateImport(elementType, store); err != nil {
		return err
	}
	o := t.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("table owner is closed")
	}
	if o.instance != nil && !o.instance.retainResourceRoot() {
		return fmt.Errorf("table owner instance is closed")
	}
	o.importers++
	return nil
}

func (t *Table) detachImporter() {
	if t == nil || t.owner == nil {
		return
	}
	o := t.owner
	var instance *Instance
	o.mu.Lock()
	if o.importers > 0 {
		o.importers--
		instance = o.instance
	}
	o.mu.Unlock()
	if instance != nil {
		instance.releaseResourceRoot()
	}
}

// retainProducerInstance transfers an instance's resource lifetime to this
// shared table when a funcref reachable through that instance remains installed
// in the table. This includes local descriptors, canonical InstanceExport slots,
// importer-owned bare-producer proxies, and HostFuncRef proxies. Retaining the
// writer preserves its existing attachment chain. Before adding the root, scan
// refSlot identities and release writers no longer represented by any entry,
// keeping retention bounded by the table's finite descriptor capacity.
func (t *Table) retainProducerInstance(in *Instance) bool {
	return t.retainProducerInstanceMode(in, false)
}

func (t *Table) retainProducerInstanceForFinalization(in *Instance) bool {
	return t.retainProducerInstanceMode(in, true)
}

func (t *Table) retainProducerInstanceMode(in *Instance, finalization bool) bool {
	if t == nil || t.owner == nil || t.owner.elementType != ValFuncRef || in == nil {
		return false
	}
	t.owner.mu.Lock()
	selfOwned := t.owner.closed || t.owner.instance == in
	t.owner.mu.Unlock()
	if selfOwned {
		return false
	}
	var retained bool
	if finalization {
		retained = in.retainResourceRootForFinalization()
	} else {
		retained = in.retainResourceRoot()
	}
	if !retained {
		return false
	}

	var release []*Instance
	t.mu.Lock()
	if t.closed || len(t.desc) < 8 {
		t.mu.Unlock()
		in.releaseResourceRoot()
		return false
	}
	for root := range t.retained {
		if !t.containsReachableFuncref(root) {
			delete(t.retained, root)
			release = append(release, root)
		}
	}
	if !t.containsReachableFuncref(in) {
		t.mu.Unlock()
		in.releaseResourceRoot()
		for _, root := range release {
			root.releaseResourceRoot()
		}
		return false
	}
	in.nativeControlShared = true
	if t.retained == nil {
		t.retained = make(map[*Instance]struct{})
	}
	_, exists := t.retained[in]
	if !exists {
		t.retained[in] = struct{}{}
	}
	t.mu.Unlock()

	if exists {
		in.releaseResourceRoot()
	}
	for _, root := range release {
		root.releaseResourceRoot()
	}
	return true
}

func (t *Table) containsReachableFuncref(in *Instance) bool {
	size := int(binary.LittleEndian.Uint32(t.desc))
	capacity := (len(t.desc) - 8) / coreruntime.TableEntryBytes
	if size > capacity {
		size = capacity
	}
	for slot := 0; slot < size; slot++ {
		off := 8 + slot*coreruntime.TableEntryBytes + coreruntime.TableEntryRefSlotOffset
		if in.reachesFuncrefDescriptor(binary.LittleEndian.Uint64(t.desc[off:])) {
			return true
		}
	}
	return false
}

// funcrefProducerRoots snapshots the roots already known to make this table's
// live descriptors callable. The descriptor owner itself is included for an
// instance-owned exported table. No store lookup occurs while Table.mu is held.
func (t *Table) funcrefProducerRoots() []*Instance {
	if t == nil || t.owner == nil {
		return nil
	}
	o := t.owner
	o.mu.Lock()
	if o.closed || o.elementType != ValFuncRef {
		o.mu.Unlock()
		return nil
	}
	instance := o.instance
	t.mu.Lock()
	roots := make([]*Instance, 0, len(t.retained)+1)
	seen := make(map[*Instance]struct{}, len(t.retained)+1)
	if !t.closed && instance != nil {
		roots = append(roots, instance)
		seen[instance] = struct{}{}
	}
	if !t.closed {
		for root := range t.retained {
			if root == nil {
				continue
			}
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}
	t.mu.Unlock()
	o.mu.Unlock()
	return roots
}

// retainDescriptorOwnersForFinalization resolves the table's current refSlots
// through the private store without holding a container lock, then adopts each
// already-retained physical owner only if the descriptor is still installed.
func (t *Table) retainDescriptorOwnersForFinalization(store *referenceStore) bool {
	if t == nil || store == nil || t.owner == nil || t.owner.elementType != ValFuncRef {
		return false
	}
	t.owner.mu.Lock()
	containerOwner := t.owner.instance
	ownerClosed := t.owner.closed
	t.owner.mu.Unlock()
	if ownerClosed {
		return false
	}
	t.mu.Lock()
	if t.closed || len(t.desc) < 8 {
		t.mu.Unlock()
		return false
	}
	descriptors := t.funcrefDescriptorsLocked()
	t.mu.Unlock()

	resolved := make(map[uint64]*Instance, len(descriptors))
	for descriptor := range descriptors {
		if owner := store.retainDescriptorOwnerForFinalization(descriptor); owner != nil {
			resolved[descriptor] = owner
		}
	}

	var release []*Instance
	t.mu.Lock()
	if t.closed || len(t.desc) < 8 {
		t.mu.Unlock()
		for _, owner := range resolved {
			owner.releaseResourceRoot()
		}
		return false
	}
	current := t.funcrefDescriptorsLocked()
	desired := make(map[*Instance]struct{}, len(resolved))
	allResolved := true
	for descriptor := range current {
		owner := resolved[descriptor]
		if owner == nil {
			allResolved = false
			continue
		}
		if owner != containerOwner {
			desired[owner] = struct{}{}
		}
	}
	for descriptor, owner := range resolved {
		if _, live := current[descriptor]; !live || owner == containerOwner {
			release = append(release, owner)
			continue
		}
		if t.retained == nil {
			t.retained = make(map[*Instance]struct{})
		}
		if _, exists := t.retained[owner]; exists {
			release = append(release, owner)
		} else {
			t.retained[owner] = struct{}{}
			owner.nativeControlShared = true
		}
	}
	for root := range t.retained {
		if _, keep := desired[root]; keep {
			continue
		}
		if !allResolved && t.containsReachableFuncref(root) {
			continue
		}
		delete(t.retained, root)
		release = append(release, root)
	}
	retained := len(t.retained) != 0
	t.mu.Unlock()
	for _, root := range release {
		root.releaseResourceRoot()
	}
	return retained
}

func (t *Table) funcrefDescriptorsLocked() map[uint64]struct{} {
	size := int(binary.LittleEndian.Uint32(t.desc))
	capacity := (len(t.desc) - 8) / coreruntime.TableEntryBytes
	if size > capacity {
		size = capacity
	}
	descriptors := make(map[uint64]struct{}, size)
	for slot := 0; slot < size; slot++ {
		off := 8 + slot*coreruntime.TableEntryBytes + coreruntime.TableEntryRefSlotOffset
		if descriptor := binary.LittleEndian.Uint64(t.desc[off:]); descriptor != 0 {
			descriptors[descriptor] = struct{}{}
		}
	}
	return descriptors
}

func (t *Table) releaseRetainedInstances() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.desc = nil
	roots := make([]*Instance, 0, len(t.retained))
	for root := range t.retained {
		roots = append(roots, root)
	}
	t.retained = nil
	t.mu.Unlock()
	for _, root := range roots {
		root.releaseResourceRoot()
	}
}

// ExportedTable returns the table exported under name as a shared *Table another
// instance can import. Compiled and codec-loaded modules resolve the declared
// export set exactly. Only legacy hand-built Compiled values keep the historical
// table-0 advisory-name fallback.
func (in *Instance) ExportedTable(name string) (*Table, error) {
	if in == nil || in.c == nil {
		return nil, fmt.Errorf("instance has no table to export")
	}
	if err := in.beginInvocation(); err != nil {
		return nil, err
	}
	defer in.endInvocation()
	tableIndex := 0
	if in.c.hasTableExportMetadata {
		var ok bool
		tableIndex, ok = in.c.tableExports[name]
		if !ok {
			return nil, fmt.Errorf("no exported table %q", name)
		}
	}
	if importDef, imported := in.c.tableImportAt(tableIndex); imported {
		table, ok := in.imports.table(importDef.Key)
		if !ok || len(table.desc) < 8 {
			return nil, fmt.Errorf("exported table %q imported descriptor is invalid", name)
		}
		return table, nil
	}
	desc := in.tableDescriptor(tableIndex)
	if len(desc) < 8 {
		return nil, fmt.Errorf("exported table %q index %d descriptor is invalid", name, tableIndex)
	}
	elementType := in.c.tableElementType(tableIndex)
	store := in.refStore
	if elementType == ValExternRef && store == nil {
		var err error
		store, err = in.referenceStoreForBoundary()
		if err != nil {
			return nil, fmt.Errorf("exported table %q reference store: %w", name, err)
		}
	}
	in.lifeMu.Lock()
	for table := in.table; table != nil; table = table.next {
		if len(table.desc) != 0 && &table.desc[0] == &desc[0] {
			in.lifeMu.Unlock()
			return table, nil
		}
	}
	owner := &tableOwner{store: store, instance: in, elementType: elementType, declaredHasMax: in.c.tableDef(tableIndex).HasMax}
	table := &Table{desc: desc, owner: owner, next: in.table}
	in.table = table
	in.lifeMu.Unlock()
	return table, nil
}

// ExportedMemory returns this instance's linear memory as a shared *Memory that
// another instance can import (cross-instance memory linking): the two instances
// then use the same underlying mapping, so stores and memory.grow are mutually
// visible. An imported-memory export forwards the exact original *Memory owner;
// it does not copy storage or create a relay lifetime. Because importers share one
// basedata region, they may not declare private globals, tables, or passive data
// state. Consumer attachments retain the original producer until the final
// importer closes. Compiler-produced modules require the exact declared export
// name; only legacy hand-built Compiled values retain the advisory-name fallback.
func (in *Instance) ExportedMemory(name string) (*Memory, error) {
	if in == nil || in.memory == nil {
		return nil, fmt.Errorf("instance has no memory to export")
	}
	if err := in.beginInvocation(); err != nil {
		return nil, err
	}
	defer in.endInvocation()
	if in.c.hasTableExportMetadata {
		if kind, ok := in.c.tableExports[name]; !ok || kind != memoryExportSentinel {
			return nil, fmt.Errorf("memory export %q not found", name)
		}
	}
	var owner *Instance
	if in.ownsMem {
		owner = in
	}
	if err := in.memory.share(owner); err != nil {
		return nil, fmt.Errorf("export memory %q: %w", name, err)
	}
	return in.memory, nil
}

// ExportedGlobalObject returns this instance's exported global `name` as a
// *Global, whose storage cell can be imported by another instance for
// cross-instance global linking (the two instances then share one cell, so
// writes are mutually visible). The referenced instance must stay open for as
// long as any importer of the global is in use. It errors if `name` is not an
// exported global.
func (in *Instance) ExportedGlobalObject(name string) (*Global, error) {
	if in == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	if err := in.beginInvocation(); err != nil {
		return nil, err
	}
	defer in.endInvocation()
	idx, ok := in.c.GlobalExports[name]
	if !ok {
		return nil, fmt.Errorf("no exported global %q", name)
	}
	if idx < 0 || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
		return nil, fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.globalCells[idx]
	if idx < len(in.c.GlobalImports) {
		return g, nil
	}
	store := in.refStore
	if isReferenceValType(g.Type) && store == nil {
		var err error
		store, err = in.referenceStoreForBoundary()
		if err != nil {
			return nil, fmt.Errorf("exported global %q reference store: %w", name, err)
		}
	}
	in.lifeMu.Lock()
	if g.owner == nil {
		g.owner = &globalOwner{store: store, instance: in, typ: g.Type, mutable: g.Mutable}
	}
	in.lifeMu.Unlock()
	return g, nil
}
