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
	mu           sync.Mutex
	arena        *coreruntime.Arena
	store        *referenceStore
	instance     *Instance
	elementType  ValType
	valueType    ValueTypeDescriptor
	types        []DefinedTypeDescriptor
	hasValueType bool
	// declaredHasMax records whether the table's external Wasm type declares an
	// explicit maximum. The runtime descriptor's capacity field is only an
	// allocation reservation (a no-max table still gets a finite reserve), so
	// import limit-matching must consult this instead of the descriptor: a table
	// with no declared maximum cannot satisfy an import that requires one.
	declaredHasMax bool
	addr64         bool // exact table index/address form; host tables are table32
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

// NewTable64 creates a bounded host-owned funcref table with 64-bit Wasm
// indices. Storage remains int-bounded and uses the same compact descriptor;
// addr64 changes validation and index operands, not the host allocation model.
func NewTable64(minSize, maxSize uint32) (*Table, error) {
	t, err := newHostTable(minSize, maxSize, ValFuncRef, nil)
	if err != nil {
		return nil, err
	}
	t.owner.addr64 = true
	return t, nil
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
	if t == nil || len(t.desc) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(t.desc))
}

// Close releases a host-created table after every importer closes. Instance-owned
// export handles remain no-ops; their producer instance owns the descriptor.
func (t *Table) Close() error {
	if t == nil || t.owner == nil || t.owner.arena == nil {
		return nil
	}
	o := t.owner
	o.mu.Lock()
	if o.closed {
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
	o.mu.Unlock()

	t.releaseRetainedInstances()
	err := arena.Close()
	t.desc = nil
	if store != nil {
		store.storeObjectClosed()
	}
	return err
}

func (t *Table) validateImport(elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, addr64 bool) error {
	if t == nil || t.owner == nil || len(t.desc) < 8 {
		return fmt.Errorf("table descriptor is invalid")
	}
	o := t.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("table owner is closed")
	}
	if o.instance != nil {
		o.instance.lifeMu.Lock()
		closed := o.instance.closed || o.instance.resourcesClosed
		o.instance.lifeMu.Unlock()
		if closed {
			return fmt.Errorf("table owner instance is closed")
		}
	}
	if o.addr64 != addr64 {
		providerBits, importBits := 32, 32
		if o.addr64 {
			providerBits = 64
		}
		if addr64 {
			importBits = 64
		}
		return fmt.Errorf("table address form mismatch: provider is table%d, import requires table%d", providerBits, importBits)
	}
	if o.elementType != elementType {
		return fmt.Errorf("table element type %s is incompatible with required %s", o.elementType, elementType)
	}
	actual := o.valueType
	actualTypes := o.types
	if !o.hasValueType {
		actual, _ = valueTypeDescriptorFromValType(o.elementType)
	}
	if !valueTypeEquivalent(actual, actualTypes, exact, types) {
		return fmt.Errorf("table exact element type is incompatible with required structural type")
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

func (t *Table) attachImporter(elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, addr64 bool) error {
	if err := t.validateImport(elementType, exact, types, store, addr64); err != nil {
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
	if t == nil || t.owner == nil || t.owner.elementType != ValFuncRef || in == nil || !in.retainResourceRoot() {
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

// pruneRetainedInstances releases closed producers whose descriptors have been
// overwritten since their roots were retained. Native table writes are complete
// before this scan runs, so a trapping operation leaves the old descriptor and
// root intact while a successful set/fill/copy/init/grow can release it.
func (t *Table) pruneRetainedInstances() {
	if t == nil {
		return
	}
	var release []*Instance
	t.mu.Lock()
	if !t.closed && len(t.desc) >= 8 {
		for root := range t.retained {
			if !t.containsReachableFuncref(root) {
				delete(t.retained, root)
				release = append(release, root)
			}
		}
	}
	t.mu.Unlock()
	for _, root := range release {
		root.releaseResourceRoot()
	}
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
	exact, err := in.c.tableExactType(tableIndex)
	if err != nil {
		in.lifeMu.Unlock()
		return nil, fmt.Errorf("exported table %q exact type: %w", name, err)
	}
	owner := &tableOwner{store: store, instance: in, elementType: elementType, valueType: exact, types: in.c.Types, hasValueType: true, declaredHasMax: in.c.tableDef(tableIndex).HasMax, addr64: in.c.tableDef(tableIndex).Addr64}
	table := &Table{desc: desc, owner: owner, next: in.table}
	in.table = table
	in.lifeMu.Unlock()
	return table, nil
}

// ExportedMemory returns the named linear memory as a shared *Memory that
// another instance can import. Imported-memory exports forward the original
// owner; local exports retain this producer until the final importer closes.
// Compiler- and codec-produced modules resolve names exactly. Legacy hand-built
// Compiled values retain the historical advisory memory-0 fallback.
func (in *Instance) ExportedMemory(name string) (*Memory, error) {
	if in == nil || in.c == nil {
		return nil, fmt.Errorf("instance has no memory to export")
	}
	memoryIndex := 0
	if in.c.hasExactMemoryExports() {
		var ok bool
		memoryIndex, ok = in.c.memoryExportMap()[name]
		if !ok {
			return nil, fmt.Errorf("no exported memory %q", name)
		}
	}
	var memory *Memory
	owns := false
	if memoryIndex == 0 {
		memory, owns = in.memory, in.ownsMem
	} else if in.memoryDir != nil && memoryIndex < len(in.memoryDir.memories) {
		memory = in.memoryDir.memories[memoryIndex]
		owns = memoryIndex < len(in.memoryDir.owns) && in.memoryDir.owns[memoryIndex]
	}
	if memory == nil {
		return nil, fmt.Errorf("exported memory %q index %d is unavailable", name, memoryIndex)
	}
	var owner *Instance
	if owns {
		owner = in
	}
	if err := memory.share(owner, in.c.memoryDef(memoryIndex)); err != nil {
		return nil, fmt.Errorf("export memory %q: %w", name, err)
	}
	return memory, nil
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
	idx, ok := in.c.GlobalExports[name]
	if !ok {
		return nil, fmt.Errorf("no exported global %q", name)
	}
	if idx < 0 || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
		return nil, fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.globalCells[idx]
	if idx < len(in.c.GlobalImports) || !isReferenceValType(g.Type) {
		return g, nil
	}
	store := in.refStore
	if store == nil {
		var err error
		store, err = in.referenceStoreForBoundary()
		if err != nil {
			return nil, fmt.Errorf("exported global %q reference store: %w", name, err)
		}
	}
	exact, err := in.c.globalExactType(idx)
	if err != nil {
		return nil, fmt.Errorf("exported global %q exact type: %w", name, err)
	}
	in.lifeMu.Lock()
	if g.owner == nil {
		g.owner = &globalOwner{store: store, instance: in, typ: g.Type, mutable: g.Mutable, valueType: exact, types: in.c.Types, hasValueType: true}
	}
	in.lifeMu.Unlock()
	return g, nil
}
