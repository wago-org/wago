package wago

import (
	"encoding/binary"
	"fmt"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
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
	in.markNativeControlShared()
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
	retained map[*Instance]*retainedInstanceRoot
}

type retainedInstanceRoot struct {
	precise          bool
	proxyDescriptors map[uint64]struct{}
}

type descriptorRetentionResult struct {
	retained   bool
	unresolved bool
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
	// Host funcref tables may be shared by scalar-only stores. Once any
	// participating store owns a Runtime GC domain, the table is pinned to that
	// store; creating a domain or adding an importer rejects a mixed-store graph.
	funcrefStores  map[*referenceStore]uint32
	funcrefGCStore *referenceStore
	importers      int
	closed         bool
}

func (t *Table) instanceOwner() *Instance {
	if t == nil || t.owner == nil {
		return nil
	}
	t.owner.mu.Lock()
	owner := t.owner.instance
	t.owner.mu.Unlock()
	return owner
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
	operation, err := rt.beginOperation("NewExternRefTable", false)
	if err != nil {
		return nil, err
	}
	defer operation.end()
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

// EntryIsNull reports whether one table entry is null without exposing its
// internal descriptor or allocating a public reference token. Callers must not
// race this diagnostic read with guest table mutation.
func (t *Table) EntryIsNull(index uint64) (bool, error) {
	if t == nil {
		return false, fmt.Errorf("wago: nil table")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || len(t.desc) < 8 || t.owner == nil {
		return false, fmt.Errorf("wago: table is closed or invalid")
	}
	size := uint64(binary.LittleEndian.Uint32(t.desc))
	if index >= size {
		return false, fmt.Errorf("wago: table index %d out of bounds (size %d)", index, size)
	}
	stride := coreruntime.TableEntryBytes
	valueOffset := 0
	if t.owner.elementType != ValFuncRef {
		stride = 8
	} else {
		valueOffset = coreruntime.TableEntryRefSlotOffset
	}
	if index > uint64((maxInt()-8-valueOffset)/stride) {
		return false, fmt.Errorf("wago: table index %d overflows host addressing", index)
	}
	offset := 8 + int(index)*stride + valueOffset
	if offset < 8 || offset+8 > len(t.desc) {
		return false, fmt.Errorf("wago: table descriptor is truncated")
	}
	return binary.LittleEndian.Uint64(t.desc[offset:]) == 0, nil
}

func (t *Table) runtimeCapacity() (uint32, bool) {
	if t == nil {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || len(t.desc) < 8 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(t.desc[4:]), true
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

//lint:ignore U1000 retained as the collector-free table import validation entrypoint
func (t *Table) validateImport(elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, addr64 bool) error {
	return t.validateImportWithCollector(elementType, exact, types, store, nil, addr64)
}

func (o *tableOwner) hasForeignFuncrefStoreLocked(store *referenceStore) bool {
	for candidate, refs := range o.funcrefStores {
		if refs != 0 && candidate != store {
			return true
		}
	}
	return false
}

func (t *Table) hasIncompatibleGCProducer(store *referenceStore) bool {
	for _, producer := range t.funcrefProducerRoots() {
		if producer == nil {
			continue
		}
		domains, topology := producer.gcInvocationDomainsForInspection()
		incompatible := false
		for i := 0; i < domains.len(); i++ {
			domain := domains.at(i)
			if domain.private || store == nil || producer.refStore != store {
				incompatible = true
				break
			}
		}
		if topology != nil {
			topology.RUnlock()
		}
		if incompatible {
			return true
		}
	}
	return false
}

func (o *tableOwner) validateFuncrefStoreLocked(store *referenceStore) error {
	if o.elementType != ValFuncRef {
		return nil
	}
	if o.instance != nil {
		if o.store != store {
			return fmt.Errorf("instance-owned funcref table requires its producer and importer in the same reference store")
		}
		return nil
	}
	if o.funcrefGCStore != nil && o.funcrefGCStore != store {
		return fmt.Errorf("host funcref table is bound to a different Runtime GC reference store")
	}
	return nil
}

func (t *Table) validateImportWithCollector(elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, collector *gc.Collector, addr64 bool) error {
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
	if err := o.validateFuncrefStoreLocked(store); err != nil {
		return err
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
	if isGCRefValType(elementType) {
		source := o.instance
		if source == nil || source.gc == nil || store == nil || collector == nil || source.gc != collector || source.refStore != store || !store.ownsGCCollector(collector) {
			return fmt.Errorf("collector-reference table requires producer and importer in the same Runtime GC domain")
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		size := uint64(binary.LittleEndian.Uint32(t.desc))
		capacity := uint64((len(t.desc) - 8) / 8)
		if size > capacity {
			return fmt.Errorf("collector-reference table length %d exceeds capacity %d", size, capacity)
		}
		for i := uint64(0); i < size; i++ {
			bits := binary.LittleEndian.Uint64(t.desc[8+i*8:])
			ref := gc.Ref(uint32(bits))
			if bits != uint64(ref) {
				return fmt.Errorf("collector-reference table slot %d contains non-compact reference %#x", i, bits)
			}
			if ref.IsNull() || ref.IsI31() {
				continue
			}
			if !ref.IsObj() {
				return fmt.Errorf("collector-reference table slot %d contains invalid reference %#x", i, bits)
			}
			if _, err := collector.ObjectType(ref); err != nil {
				return fmt.Errorf("collector-reference table slot %d contains stale or foreign object: %w", i, err)
			}
		}
	}
	return nil
}

func (t *Table) attachImporter(elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, addr64 bool) error {
	return t.attachImporterWithCollector(elementType, exact, types, store, nil, addr64)
}

func (t *Table) attachImporterWithCollector(elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, collector *gc.Collector, addr64 bool) error {
	if err := t.validateImportWithCollector(elementType, exact, types, store, collector, addr64); err != nil {
		return err
	}
	o := t.owner
	if o.elementType == ValFuncRef && o.instance == nil {
		if t.hasIncompatibleGCProducer(store) {
			return fmt.Errorf("funcref table contains a producer from an incompatible GC invocation domain")
		}
		var topology *gcDomainTopology
		if store != nil {
			topology = store.ensureGCTopology()
			topology.funcrefMu.Lock()
			defer topology.funcrefMu.Unlock()
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.closed {
			return fmt.Errorf("table owner is closed")
		}
		if err := o.validateFuncrefStoreLocked(store); err != nil {
			return err
		}
		if store == nil {
			if o.hasForeignFuncrefStoreLocked(nil) {
				return fmt.Errorf("standalone and Runtime instances cannot concurrently share a mutable funcref table")
			}
		} else if o.funcrefStores[nil] != 0 {
			return fmt.Errorf("standalone and Runtime instances cannot concurrently share a mutable funcref table")
		}
		if topology != nil && topology.funcrefGCActive {
			if o.hasForeignFuncrefStoreLocked(store) {
				return fmt.Errorf("Runtime GC domains cannot share a mutable funcref table across reference stores")
			}
			o.funcrefGCStore = store
		}
		if o.funcrefStores[store] == ^uint32(0) {
			return fmt.Errorf("funcref table has too many importers in one reference store")
		}
		if topology != nil && topology.funcrefTables[t] == ^uint32(0) {
			return fmt.Errorf("funcref table has too many importers in one Runtime")
		}
		if o.funcrefStores == nil {
			o.funcrefStores = make(map[*referenceStore]uint32)
		}
		o.funcrefStores[store]++
		if topology != nil {
			if topology.funcrefTables == nil {
				topology.funcrefTables = make(map[*Table]uint32)
			}
			topology.funcrefTables[t]++
		}
		o.importers++
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("table owner is closed")
	}
	if err := o.validateFuncrefStoreLocked(store); err != nil {
		return err
	}
	if o.instance != nil && !o.instance.retainResourceRoot() {
		return fmt.Errorf("table owner instance is closed")
	}
	o.importers++
	return nil
}

func (t *Table) detachImporter(store *referenceStore) {
	if t == nil || t.owner == nil {
		return
	}
	o := t.owner
	if o.elementType == ValFuncRef && o.instance == nil {
		var topology *gcDomainTopology
		if store != nil {
			store.mu.Lock()
			topology = store.gcDomains
			store.mu.Unlock()
			if topology != nil {
				topology.funcrefMu.Lock()
				defer topology.funcrefMu.Unlock()
			}
		}
		o.mu.Lock()
		if o.importers > 0 {
			o.importers--
			if refs := o.funcrefStores[store]; refs <= 1 {
				delete(o.funcrefStores, store)
			} else {
				o.funcrefStores[store] = refs - 1
			}
			if topology != nil {
				if refs := topology.funcrefTables[t]; refs <= 1 {
					delete(topology.funcrefTables, t)
				} else {
					topology.funcrefTables[t] = refs - 1
				}
			}
		}
		o.mu.Unlock()
		return
	}
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
	current := t.funcrefDescriptorsLocked()
	for root, state := range t.retained {
		state.precise = state.precise && t.containsReachableFuncref(root)
		for descriptor := range state.proxyDescriptors {
			if _, live := current[descriptor]; !live {
				delete(state.proxyDescriptors, descriptor)
			}
		}
		if !state.precise && len(state.proxyDescriptors) == 0 {
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
	in.markNativeControlShared()
	if t.retained == nil {
		t.retained = make(map[*Instance]*retainedInstanceRoot)
	}
	state, exists := t.retained[in]
	if !exists {
		state = &retainedInstanceRoot{}
		t.retained[in] = state
	}
	state.precise = true
	for root, retained := range t.retained {
		for descriptor := range retained.proxyDescriptors {
			if in.reachesFuncrefDescriptor(descriptor) {
				delete(retained.proxyDescriptors, descriptor)
			}
		}
		if !retained.precise && len(retained.proxyDescriptors) == 0 {
			delete(t.retained, root)
			release = append(release, root)
		}
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

// retainDescriptorOwnersForFinalization snapshots the table's current refSlots,
// resolves them without holding a container lock, then reconciles ownership only
// if those descriptors are still installed. Descriptors outside store remain
// unresolved; in that case proxy is retained even when its own descriptor arena
// does not contain the copied refSlot. The proxy's still-live import attachments
// conservatively preserve the source chain until overwrite or table close.
func (t *Table) retainDescriptorOwnersForFinalization(store *referenceStore, proxy *Instance) descriptorRetentionResult {
	if t == nil || t.owner == nil || t.owner.elementType != ValFuncRef {
		return descriptorRetentionResult{}
	}
	t.owner.mu.Lock()
	containerOwner := t.owner.instance
	ownerClosed := t.owner.closed
	t.owner.mu.Unlock()
	if ownerClosed {
		return descriptorRetentionResult{}
	}
	t.mu.Lock()
	if t.closed || len(t.desc) < 8 {
		t.mu.Unlock()
		return descriptorRetentionResult{}
	}
	descriptors := t.funcrefDescriptorsLocked()
	t.mu.Unlock()

	resolved := make(map[uint64]*Instance, len(descriptors))
	if store != nil {
		for descriptor := range descriptors {
			if owner := store.retainDescriptorOwnerForFinalization(descriptor); owner != nil {
				resolved[descriptor] = owner
			}
		}
	}
	unresolvedSnapshot := len(resolved) != len(descriptors)
	proxyAcquired := unresolvedSnapshot && proxy != nil && proxy != containerOwner && proxy.retainResourceRootForFinalization()

	var release []*Instance
	t.mu.Lock()
	if t.closed || len(t.desc) < 8 {
		t.mu.Unlock()
		for _, owner := range resolved {
			owner.releaseResourceRoot()
		}
		if proxyAcquired {
			proxy.releaseResourceRoot()
		}
		return descriptorRetentionResult{}
	}
	current := t.funcrefDescriptorsLocked()
	unresolved := make(map[uint64]struct{})
	for descriptor := range current {
		if resolved[descriptor] == nil {
			unresolved[descriptor] = struct{}{}
		}
	}
	for root, state := range t.retained {
		state.precise = false
		for descriptor, owner := range resolved {
			if owner == root {
				if _, live := current[descriptor]; live {
					state.precise = true
					break
				}
			}
		}
		if !state.precise {
			for descriptor := range unresolved {
				if root.reachesFuncrefDescriptor(descriptor) {
					state.precise = true
					break
				}
			}
		}
		for descriptor := range state.proxyDescriptors {
			if _, keep := unresolved[descriptor]; !keep {
				delete(state.proxyDescriptors, descriptor)
			}
		}
		if !state.precise && len(state.proxyDescriptors) == 0 {
			delete(t.retained, root)
			release = append(release, root)
		}
	}
	for descriptor, owner := range resolved {
		if _, live := current[descriptor]; !live || owner == containerOwner {
			release = append(release, owner)
			continue
		}
		if t.retained == nil {
			t.retained = make(map[*Instance]*retainedInstanceRoot)
		}
		state := t.retained[owner]
		if state == nil {
			state = &retainedInstanceRoot{}
			t.retained[owner] = state
			owner.markNativeControlShared()
		} else {
			release = append(release, owner)
		}
		state.precise = true
	}
	for descriptor := range unresolved {
		precise := false
		for root, state := range t.retained {
			if state.precise && root.reachesFuncrefDescriptor(descriptor) {
				precise = true
				break
			}
		}
		if precise {
			for _, state := range t.retained {
				delete(state.proxyDescriptors, descriptor)
			}
		}
	}
	if proxyAcquired {
		needed := make(map[uint64]struct{}, len(unresolved))
		for descriptor := range unresolved {
			needed[descriptor] = struct{}{}
		}
		for root, state := range t.retained {
			for descriptor := range needed {
				_, proxied := state.proxyDescriptors[descriptor]
				if proxied || (state.precise && root.reachesFuncrefDescriptor(descriptor)) {
					delete(needed, descriptor)
				}
			}
		}
		if len(needed) == 0 {
			release = append(release, proxy)
		} else {
			if t.retained == nil {
				t.retained = make(map[*Instance]*retainedInstanceRoot)
			}
			state := t.retained[proxy]
			if state == nil {
				state = &retainedInstanceRoot{}
				t.retained[proxy] = state
				proxy.markNativeControlShared()
			} else {
				release = append(release, proxy)
			}
			if state.proxyDescriptors == nil {
				state.proxyDescriptors = make(map[uint64]struct{}, len(needed))
			}
			for descriptor := range needed {
				state.proxyDescriptors[descriptor] = struct{}{}
			}
		}
	}
	for root, state := range t.retained {
		if !state.precise && len(state.proxyDescriptors) == 0 {
			delete(t.retained, root)
			release = append(release, root)
		}
	}
	result := descriptorRetentionResult{retained: len(t.retained) != 0, unresolved: len(unresolved) != 0}
	t.mu.Unlock()
	for _, root := range release {
		root.releaseResourceRoot()
	}
	return result
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

// pruneRetainedInstances reconciles precise and proxy roots after a completed
// table mutation. Proxy descriptor sets remain bounded by live table capacity.
func (t *Table) pruneRetainedInstances() {
	if t == nil {
		return
	}
	var release []*Instance
	t.mu.Lock()
	if !t.closed && len(t.desc) >= 8 {
		current := t.funcrefDescriptorsLocked()
		for root, state := range t.retained {
			state.precise = false
			for descriptor := range current {
				if root.reachesFuncrefDescriptor(descriptor) {
					state.precise = true
					break
				}
			}
			for descriptor := range state.proxyDescriptors {
				if _, live := current[descriptor]; !live {
					delete(state.proxyDescriptors, descriptor)
				}
			}
			if !state.precise && len(state.proxyDescriptors) == 0 {
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
	if (elementType == ValExternRef || isGCRefValType(elementType)) && store == nil {
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
			in.markNativeControlShared()
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
	in.markNativeControlShared()
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
	if err := in.beginInvocation(); err != nil {
		return nil, err
	}
	defer in.endInvocation()
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
	if owns {
		in.markNativeControlShared()
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
	exact, err := in.c.globalExactType(idx)
	if err != nil {
		return nil, fmt.Errorf("exported global %q exact type: %w", name, err)
	}
	in.lifeMu.Lock()
	if g.owner == nil {
		g.owner = &globalOwner{store: store, instance: in, typ: g.Type, mutable: g.Mutable, valueType: exact, types: in.c.Types, hasValueType: true}
	}
	in.lifeMu.Unlock()
	in.markNativeControlShared()
	return g, nil
}
