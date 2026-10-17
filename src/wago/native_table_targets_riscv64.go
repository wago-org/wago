//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Retained ownership for public owned/imported canonical table calls.
// A caller refreshes under nativeExecutionMu before entry and every resume,
// retains this set through cold decoration, then releases outside that mutex.
// Refresh merges new targets without dropping suspended activations' old roots.
// Container-held Go pointers alone are not a physical activation lifetime.
type rv64NativeTableTargets struct {
	owners          []*Instance
	smallOwners     [8]*Instance
	ownerIndex      map[*Instance]struct{} // only large scopes need an index
	writers         []rv64TableWriter
	storage         []*Table
	smallWriters    [8]rv64TableWriter
	smallStorage    [8]*Table
	count           int
	tailBudget      rv64NativeTailGenerationBudget
	tailGenerations []*rv64NativeTailGeneration // all versions, including suspended code
	tailActive      []*rv64NativeTailGeneration
	tailOwnerCount  int
	tailRetiring    bool
}

type rv64TableWriter struct {
	table *Table
	owner uint16 // index in the retained owner directory
}

const rv64MaxTableLeaseEdges = 4096

func (s *rv64NativeTableTargets) add(owner *Instance) error {
	if owner == nil {
		return fmt.Errorf("RV64 table target owner is nil")
	}
	if s.ownerIndex != nil {
		if _, found := s.ownerIndex[owner]; found {
			return nil
		}
	} else {
		for _, existing := range s.owners[:s.count] {
			if existing == owner {
				return nil
			}
		}
	}
	if s.count == rv64MaxTableLeaseEdges+1 {
		return fmt.Errorf("RV64 table target owner bound")
	}
	if s.owners == nil {
		s.owners = s.smallOwners[:]
	}
	if s.count == len(s.owners) {
		capacity := min(2*len(s.owners), rv64MaxTableLeaseEdges+1)
		owners := make([]*Instance, capacity)
		copy(owners, s.owners)
		s.owners = owners
	}
	if s.count == 32 && s.ownerIndex == nil {
		s.ownerIndex = make(map[*Instance]struct{}, s.count+1)
		for _, existing := range s.owners[:s.count] {
			s.ownerIndex[existing] = struct{}{}
		}
	}
	// Existing table/direct attachments authorize use after logical Close. The
	// container lock or a retained predecessor still owns physical storage here.
	if !owner.retainResourceRootForFinalization() {
		return fmt.Errorf("RV64 table target lost physical resources")
	}
	s.owners[s.count] = owner
	s.count++
	if s.ownerIndex != nil {
		s.ownerIndex[owner] = struct{}{}
	}
	return nil
}

func (s *rv64NativeTableTargets) release() {
	if s.tailBudget != (rv64NativeTailGenerationBudget{}) {
		panic("RV64 tail generations must retire before scope roots")
	}
	var cleanup resourceCleanup
	cleanup.run(s.publishCrossCopyReferenceRows)
	publishWriters, publishForeignWriters := s.writerPublicationMode()
	// Remove every transient promise before persistent publication. A reachable
	// producer can share a source table with the public foreign writer; pruning
	// either promise early would overwrite the later proxy handoff.
	for _, writer := range s.writers {
		table := writer.table
		if table == nil {
			continue
		}
		table.mu.Lock()
		state := table.retained[s.owners[writer.owner]]
		if state == nil || state.activeWriters == 0 {
			panic("RV64 table writer release order")
		}
		state.activeWriters--
		table.mu.Unlock()
	}
	if publishWriters {
		for _, writer := range s.writers {
			cleanup.run(func() { s.publishWriterReferenceRows(writer, publishForeignWriters) })
		}
	}
	// Prune after every proxy is stable. Duplicate table visits are harmless and
	// keep the fixed small-slice journal allocation-free.
	for index, writer := range s.writers {
		table := writer.table
		if table == nil {
			continue
		}
		s.writers[index] = rv64TableWriter{}
		cleanup.run(table.pruneRetainedInstancesRV64)
	}
	s.writers = s.writers[:0]
	for index, table := range s.storage {
		if table == nil {
			continue
		}
		s.storage[index] = nil
		table.owner.mu.Lock()
		if table.owner.nativeLeases == 0 {
			panic("RV64 table storage release order")
		}
		table.owner.nativeLeases--
		parent := table.owner.instance
		table.owner.mu.Unlock()
		if parent != nil {
			cleanup.run(parent.releaseResourceRoot)
		}
		cleanup.run(func() { railshotFinishCompactExternHostTableDetach(table) })
	}
	s.storage = s.storage[:0]
	// Releasing the last retired root can reenter finalization/native locking.
	for s.count != 0 {
		s.count--
		owner := s.owners[s.count]
		s.owners[s.count] = nil
		delete(s.ownerIndex, owner)
		cleanup.run(owner.releaseResourceRoot)
	}
	cleanup.finish()
}

func (s *rv64NativeTableTargets) owner(identity uintptr) *Instance {
	if identity == 0 {
		return nil
	}
	for _, owner := range s.owners[:s.count] {
		if owner.nativeContext == identity {
			return owner
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) tableRoots(table *Table, edges *int) error {
	if table == nil || table.owner == nil {
		return fmt.Errorf("RV64 retained table has no owner")
	}
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	// Host-owned table64 descriptors have no compiled instance from which to
	// recover a source-profile witness. Their address form is fixed by
	// NewTable64 and the import declaration is independently qualified before
	// this retained-target walk. Instance-owned providers still require their
	// exact compiled table64 witness.
	qualifiedTable64 := table.owner.addr64 && (table.owner.instance == nil || rv64QualifiedTable64(table.owner.instance.c))
	if table.owner.closed || table.closed || table.owner.addr64 && !qualifiedTable64 || table.owner.elementType != ValFuncRef || len(table.desc) < 8 {
		return fmt.Errorf("RV64 retained table storage/type is invalid (closed=%t/%t addr64=%t qualified=%t element=%v bytes=%d)", table.owner.closed, table.closed, table.owner.addr64, qualifiedTable64, table.owner.elementType, len(table.desc))
	}
	if table.owner.instance != nil {
		if err := s.add(table.owner.instance); err != nil {
			return err
		}
	}
	for owner := range table.retained {
		*edges++
		if *edges > 4096 {
			return fmt.Errorf("RV64 table attachment edge bound")
		}
		if err := s.add(owner); err != nil {
			return err
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) refresh(root *Instance) error {
	return s.refreshWithArguments(root, nil)
}

func (s *rv64NativeTableTargets) refreshWithArguments(root *Instance, arguments *rv64InvocationArgumentOwners) error {
	if s.count != 0 && s.owners[0] != root {
		return fmt.Errorf("RV64 table target set cannot change its root")
	}
	if err := s.add(root); err != nil {
		return err
	}
	if arguments != nil {
		if arguments.count < 0 || arguments.count > len(arguments.owners) || !arguments.active && arguments.count != 0 {
			return fmt.Errorf("RV64 reference argument owner scope is invalid")
		}
		if arguments.active {
			for _, owner := range arguments.owners[:arguments.count] {
				if err := s.add(owner); err != nil {
					return err
				}
			}
		}
	}
	return s.validateReachable()
}

// addMutableLocalCallTarget promotes the exact mutable cell's current callable
// owner from a container root into this activation. Native execution is leased
// by the caller, so a parked callback may replace the cell only after this
// independent root is installed. The old owner then remains physical until the
// activation resumes and unwinds, even when it was already logically closed.
func (s *rv64NativeTableTargets) addMutableLocalCallTarget(root *Instance) error {
	if root == nil {
		return nil
	}
	if rv64PrimitiveReferenceContract(root.c) {
		for index, global := range root.c.Globals {
			if global.Type != ValFuncRef || !global.Mutable {
				continue
			}
			if root.refStore == nil || index >= len(root.globalCells) {
				return fmt.Errorf("RV64 mutable call target cell is unavailable")
			}
			if err := s.addMutableGlobalCallTarget(root, index); err != nil {
				return err
			}
		}
		return nil
	}
	if !rv64MutableFuncrefGlobalLocalCall(root.c) {
		return nil
	}
	if len(root.globalCells) != 1 || root.refStore == nil {
		return fmt.Errorf("RV64 mutable call target cell is unavailable")
	}
	return s.addMutableGlobalCallTarget(root, 0)
}

func descriptorBelongsToInstance(owner *Instance, descriptor uint64) bool {
	_, ok := owner.funcrefDescriptorIndex(descriptor)
	return ok
}

func (s *rv64NativeTableTargets) addMutableGlobalCallTarget(root *Instance, index int) error {
	cell := root.globalCells[index]
	if cell == nil || cell.Type != ValFuncRef || !cell.Mutable || len(cell.cell) < 8 {
		return fmt.Errorf("RV64 mutable call target cell metadata changed")
	}
	root.lifeMu.Lock()
	o := cell.owner
	root.lifeMu.Unlock()
	if o == nil {
		descriptor := readGlobalObject(cell, ValFuncRef)
		if descriptor != 0 {
			if _, local := root.funcrefDescriptorIndex(descriptor); !local {
				return fmt.Errorf("RV64 ownerless mutable call target is foreign")
			}
		}
		// Local reference cells acquire ownership lazily on their first store or
		// host object access. The authenticated initializer is null, so the first
		// call must reach call_ref and report the ordinary null-reference trap.
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.store != root.refStore || o.typ != ValFuncRef || !o.mutable || cell.Type != ValFuncRef || !cell.Mutable || len(cell.cell) < 8 {
		return fmt.Errorf("RV64 mutable call target cell metadata changed")
	}
	descriptor := readGlobalObject(cell, ValFuncRef)
	var target *Instance
	scopedTarget := false
	if descriptor == 0 {
		if len(o.retained) != 0 {
			return fmt.Errorf("RV64 null mutable call target retained an owner")
		}
	} else if _, local := root.funcrefDescriptorIndex(descriptor); local {
		for owner, state := range o.retained {
			if owner != root || state == nil || !state.precise || state.activeWriters != 0 || len(state.proxyDescriptors) != 0 {
				return fmt.Errorf("RV64 local mutable call target retained a foreign owner")
			}
		}
		target = root
	} else if o.instance != nil && descriptorBelongsToInstance(o.instance, descriptor) {
		target = o.instance
	} else {
		if len(o.retained) != 1 {
			return fmt.Errorf("RV64 foreign mutable call target has no precise owner")
		}
		for owner, state := range o.retained {
			if owner == nil || state == nil || state.activeWriters != 0 {
				return fmt.Errorf("RV64 foreign mutable call target ownership is imprecise")
			}
			if state.precise && len(state.proxyDescriptors) == 0 {
				target = owner
				scopedTarget = true
				break
			}
			if _, proxied := state.proxyDescriptors[descriptor]; !proxied {
				return fmt.Errorf("RV64 foreign mutable call target ownership is imprecise")
			}
			for _, candidate := range s.owners[:s.count] {
				if _, valid := rv64PublishedDescriptor(candidate, descriptor); !valid {
					continue
				}
				if target != nil && target != candidate {
					return fmt.Errorf("RV64 foreign mutable call target owner is ambiguous")
				}
				target = candidate
			}
			scopedTarget = true
		}
	}
	if descriptor == 0 {
		return nil
	}
	if target != nil && target.refStore != root.refStore {
		for _, candidate := range s.owners[:s.count] {
			if candidate != target {
				continue
			}
			if _, valid := rv64PublishedDescriptor(candidate, descriptor); valid {
				scopedTarget = true
			}
			break
		}
	}
	if target == nil || !scopedTarget && target.refStore != root.refStore {
		return fmt.Errorf("RV64 mutable call target owner changed")
	}
	if _, valid := rv64PublishedDescriptor(target, descriptor); !valid {
		return fmt.Errorf("RV64 mutable call target descriptor changed")
	}
	expected, err := root.c.globalExactType(index)
	actual, actualTypes, typed := instanceFuncrefExactType(target, descriptor)
	if err != nil || !typed || !valueTypeSubtype(actual, actualTypes, expected, root.c.Types) {
		return fmt.Errorf("RV64 mutable call target type changed")
	}
	return s.add(target)
}

// The caller holds nativeExecutionMu and releases roots only after unlocking.
func (s *rv64NativeTableTargets) validateReachable() error {
	edges := 0
	for cursor := 0; cursor < s.count; cursor++ {
		owner := s.owners[cursor]
		stateOnly := owner.c != nil && len(owner.c.Funcs) == 0 && len(owner.c.Entry) == 0 && len(owner.c.InternalEntry) == 0 && len(owner.c.code) == 0
		if owner.c == nil || !stateOnly && (owner.jm == nil || owner.base == 0 || owner.nativeContext == 0) || owner.hostEvents != nil && cursor != 0 || !owner.c.nativeContextTrapABI {
			return fmt.Errorf("RV64 table target requires a retained native framed owner")
		}
		if !stateOnly {
			if _, err := (rv64TailCodeOwner{owner}).nativeOwnerCodeView(true); err != nil {
				return err
			}
		}
		if err := owner.c.validateCached(); err != nil {
			return err
		}
		if rv64StorageFreeCallable(owner.c) {
			continue // construction-only storage attachments may already be transferred
		}
		if err := s.addMutableLocalCallTarget(owner); err != nil {
			return err
		}
		// The immutable owner snapshot has already authenticated every table's
		// type and layout. Keep the reachability walk bounded without repeating
		// source-profile hashing or rebuilding type graphs on this hot path.
		if owner.c.tableCount() > railshot.MaxFunctions {
			return fmt.Errorf("RV64 table target table directory bound")
		}

		for index := range owner.c.Imports {
			edges++
			if edges > 4096 {
				return fmt.Errorf("RV64 table attachment edge bound")
			}
			if export, native := rv64NativeFunctionAttachment(owner, index); native && export != nil {
				if err := s.add(export.inst); err != nil {
					return err
				}
			}
		}
		for _, entry := range owner.importGlobalReferences() {
			edges++
			if edges > 4096 {
				return fmt.Errorf("RV64 reference attachment edge bound")
			}
			descriptor, _, err := rv64ReferenceGlobalSnapshot(entry.global, owner.refStore, true)
			if err != nil || descriptor != entry.descriptor {
				return fmt.Errorf("RV64 immutable reference attachment changed")
			}
			if descriptor != 0 {
				if err := s.add(entry.owner); err != nil {
					return err
				}
				if _, valid := rv64PublishedDescriptor(entry.owner, descriptor); !valid {
					return fmt.Errorf("RV64 reference attachment descriptor changed")
				}
			}
		}
		for index := 0; index < owner.c.tableImportCount(); index++ {
			table, ok := rv64NativeTableAttachment(owner, index)
			if !ok {
				return fmt.Errorf("RV64 retained imported table is missing")
			}
			// Compact collector-reference rows are roots in the shared GC
			// domain, not executable target descriptors. Their importer
			// attachment already retains storage and their rows are validated
			// below with the compiled table stride.
			if table == nil || table.owner == nil {
				return fmt.Errorf("RV64 retained imported table has no owner")
			}
			if rv64ClosedImportedTableAttachment(owner, index) {
				continue
			}
			if table.owner.elementType != ValFuncRef {
				continue
			}
			if err := s.tableRoots(table, &edges); err != nil {
				return err
			}
			if err := s.retainImportedTable(table, cursor); err != nil {
				return err
			}
		}
		owner.lifeMu.Lock()
		table := owner.table
		owner.lifeMu.Unlock()
		for table != nil {
			if table.owner != nil && table.owner.elementType == ValFuncRef {
				edges++
				if edges > 4096 {
					return fmt.Errorf("RV64 table attachment edge bound")
				}
				if err := s.tableRoots(table, &edges); err != nil {
					return err
				}
			}
			owner.lifeMu.Lock()
			table = table.next
			owner.lifeMu.Unlock()
		}
	}
	// Only after acquiring every reachable physical root do we examine rows.
	// Raw refSlot/context fields never authorize dereferencing unknown storage.
	for _, owner := range s.owners[:s.count] {
		if err := rv64ValidateTailTargets(owner); err != nil {
			return err
		}
		if rv64StorageFreeCallable(owner.c) {
			continue
		}
		for tableIndex := 0; tableIndex < owner.c.tableCount(); tableIndex++ {
			if rv64ClosedImportedTableAttachment(owner, tableIndex) {
				continue
			}
			if err := s.validateTable(rv64RetainedTableDescriptor(owner, tableIndex), owner.c.tableEntryBytes(tableIndex)); err != nil {
				return err
			}
		}
	}
	return nil
}

func rv64ClosedImportedTableAttachment(owner *Instance, index int) bool {
	if owner == nil || owner.c == nil || !owner.isLogicallyClosed() {
		return false
	}
	if _, imported := owner.c.tableImportAt(index); !imported {
		return false
	}
	table, ok := rv64NativeTableAttachment(owner, index)
	if !ok || table == nil || table.owner == nil {
		return false
	}
	table.owner.mu.Lock()
	table.mu.Lock()
	closed := table.owner.closed || table.closed
	table.mu.Unlock()
	table.owner.mu.Unlock()
	return closed
}

// Local descriptors normally belong to the retained instance arena. Reuse its
// slice provenance on warm validation, including under pointer instrumentation.
// Imported or separately owned/grown storage keeps the existing owner accessor.
func rv64RetainedTableDescriptor(owner *Instance, index int) []byte {
	if index < 0 || index >= owner.c.tableCount() {
		return nil
	}
	if _, imported := owner.c.tableImportAt(index); imported {
		table, ok := rv64NativeTableAttachment(owner, index)
		if !ok || table == nil || len(table.desc) < 8 {
			return nil
		}
		return table.desc
	}
	address, size := owner.tableDescPtr, owner.tableDescLen
	if index != 0 {
		directory := owner.ar.AllocatedRange(owner.jm.TableDirPtr(), 8*owner.c.tableCount())
		if len(directory) == 0 {
			return owner.tableDescriptor(index)
		}
		address = uintptr(binary.LittleEndian.Uint64(directory[index*8:]))
		size = 8 + owner.c.tableRuntimeCapacity(index)*owner.c.tableEntryBytes(index)
	}
	if descriptor := owner.ar.AllocatedRange(address, size); len(descriptor) != 0 {
		return descriptor
	}
	return owner.tableDescriptor(index)
}

func (s *rv64NativeTableTargets) validateTable(table []byte, entryBytes int) error {
	if len(table) < 8 {
		return fmt.Errorf("RV64 table descriptor extent")
	}
	if entryBytes != 8 && entryBytes != coreruntime.TableEntryBytes {
		return fmt.Errorf("RV64 table row width")
	}
	size, capacity := binary.LittleEndian.Uint32(table), binary.LittleEndian.Uint32(table[4:])
	if size > capacity || uint64(8)+uint64(capacity)*uint64(entryBytes) > uint64(len(table)) {
		return fmt.Errorf("RV64 table descriptor capacity/extent")
	}
	// Compact reference rows are opaque store-owned tokens, never executable
	// descriptors. Public argument and result boundaries validate their store
	// identity; this retained-target walk must not interpret token bits as code,
	// signature, context, or owner pointers.
	if entryBytes == 8 {
		return nil
	}
	for index := uint32(0); index < size; index++ {
		row := table[8+int(index)*coreruntime.TableEntryBytes : 8+int(index+1)*coreruntime.TableEntryBytes]
		if binary.LittleEndian.Uint64(row) == 0 {
			var empty [coreruntime.TableEntryBytes]byte
			if !bytes.Equal(row, empty[:]) {
				return fmt.Errorf("RV64 null table row has nonzero identity")
			}
			continue
		}
		if !s.authenticatedRow(row) {
			return fmt.Errorf("RV64 table row lacks a qualified retained canonical owner")
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) authenticatedRow(row []byte) bool {
	descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
	for _, owner := range s.owners[:s.count] {
		canonical, ok := rv64PublishedDescriptor(owner, descriptor)
		if ok {
			return bytes.Equal(row, canonical[:coreruntime.TableEntryBytes])
		}
	}
	return false
}

// Membership is established against retained storage before reading a slot.
// Both token and native-entry paths authenticate the complete publication ABI.
func rv64PublishedDescriptor(owner *Instance, descriptor uint64) ([]byte, bool) {
	if owner == nil || owner.c == nil || owner.jm == nil || owner.nativeContext == 0 || descriptor == 0 || descriptor&7 != 0 {
		return nil, false
	}
	function, ok := owner.funcrefDescriptorIndex(descriptor)
	if !ok || railshotNonCallableFunction(owner.c, function) {
		return nil, false
	}
	offset := (function + 1) * coreruntime.FuncRefDescBytes
	if offset+coreruntime.FuncRefDescBytes > len(owner.funcRefDescs) {
		return nil, false
	}
	local := function - owner.c.NumImports
	var entry uint64
	if local < 0 {
		if function >= len(owner.importReferenceEntries()) {
			return nil, false
		}
		entry = owner.importReferenceEntries()[function].address
		if entry == 0 || entry&15 != 0 {
			return nil, false
		}
	} else {
		if local >= len(owner.c.Entry) || local >= len(owner.c.Funcs) {
			return nil, false
		}
		code := owner.c.Entry[local]
		indexes := owner.c.loadCompileIndexes()
		if owner.base == 0 || code < 0 || code&15 != 0 || indexes == nil || code >= indexes.railshotTail.mappedCodeBytes {
			return nil, false
		}
		entry = uint64(owner.base) + uint64(code)
	}
	key := rv64AttachedFunctionTypeKey(owner, function)
	if key == 0 {
		return nil, false
	}
	canonical := owner.funcRefDescs[offset : offset+coreruntime.FuncRefDescBytes]
	valid := binary.LittleEndian.Uint64(canonical) == entry &&
		binary.LittleEndian.Uint64(canonical[coreruntime.TableEntrySigKeyOffset:]) == key &&
		binary.LittleEndian.Uint64(canonical[coreruntime.TableEntryHomeLinMemOffset:]) == uint64(owner.jm.LinMemBase())|abi.FuncRefLocalWrapperHomeTag &&
		binary.LittleEndian.Uint64(canonical[coreruntime.TableEntryRefSlotOffset:]) == descriptor &&
		binary.LittleEndian.Uint64(canonical[coreruntime.FuncRefContextOffset:]) == uint64(owner.nativeContext)
	return canonical, valid
}

// Publish a newly constructed writer while native execution is leased, without
// pruning prior writers: their final-root release can reacquire that lease.
// The caller releases the returned redundant root and prunes after unlocking.
func railshotPublishConstructedTableProducer(table *Table, in *Instance) *Instance {
	// Active elements can copy descriptors from imported immutable globals
	// before the first native invocation. Frozen global attachments already
	// pin and quiesce these actual owners; publish their exact rows even when
	// a later initializer failed. No root may be released under the native lease.
	// This borrowed candidate set is not an activation scope: do not add/release.
	if references := in.importGlobalReferences(); len(references) != 0 {
		var candidates rv64NativeTableTargets
		candidates.owners = candidates.smallOwners[:]
		for _, reference := range references {
			if reference.owner == nil {
				continue
			}
			duplicate := false
			for _, owner := range candidates.owners[:candidates.count] {
				duplicate = duplicate || owner == reference.owner
			}
			if !duplicate {
				// Frozen attachments retain all owners. Publish bounded batches
				// without a second allocation for large global directories.
				if candidates.count == len(candidates.owners) {
					candidates.publishHostTableReferenceRows(table)
					clear(candidates.owners[:])
					candidates.count = 0
				}
				candidates.owners[candidates.count] = reference.owner
				candidates.count++
			}
		}
		candidates.publishHostTableReferenceRows(table)
	}
	if !in.retainResourceRoot() {
		return nil
	}
	in.markNativeControlShared()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.closed || len(table.desc) < 8 || !table.containsReachableFuncref(in) {
		return in
	}
	if table.retained == nil {
		table.retained = make(map[*Instance]*retainedInstanceRoot)
	}
	if table.retained[in] != nil {
		return in
	}
	state := &retainedInstanceRoot{precise: true}
	table.retained[in] = state
	// Native construction still owns outer serialization. Record the stable
	// bootstrap only; the immediately following prune publishes after unlock.
	railshotRecordTableDescriptorDependencyLocked(table, in, state, nil)
	return nil
}

func railshotLockNativeTableScan()   { nativeExecutionMu.Lock() }
func railshotUnlockNativeTableScan() { nativeExecutionMu.Unlock() }

// A suspended scope can create local refs after resume, even if its current
// rows are empty. Container membership remains until every writer scope leaves.
func (s *rv64NativeTableTargets) activateWriter(table *Table, owner *Instance) error {
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.closed || len(table.desc) < 8 {
		return fmt.Errorf("RV64 writer table storage is invalid")
	}
	state := table.retained[owner]
	if state != nil && state.activeWriters == ^uint32(0) {
		return fmt.Errorf("RV64 table writer counter bound")
	}
	if state == nil {
		if !owner.retainResourceRootForFinalization() {
			return fmt.Errorf("RV64 table writer lost physical storage")
		}
		if table.retained == nil {
			table.retained = make(map[*Instance]*retainedInstanceRoot)
		}
		state = &retainedInstanceRoot{}
		table.retained[owner] = state
		// The active writer root is conservatively external until scope release
		// invokes prune outside native serialization.
		railshotRecordTableDescriptorDependencyLocked(table, owner, state, nil)
	}
	state.activeWriters++
	return nil
}

// This lease is distinct from an ordinary importer attachment, which a retired
// writer may transfer to its table while a native scope is still suspended.
func (table *Table) acquireRV64NativeStorage() error {
	if table == nil || table.owner == nil {
		return fmt.Errorf("RV64 table storage has no owner")
	}
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner.closed || table.closed || len(table.desc) < 8 || table.owner.nativeLeases == ^uint32(0) {
		return fmt.Errorf("RV64 table storage lease is invalid or exhausted")
	}
	if parent := table.owner.instance; parent != nil && !parent.retainResourceRootForFinalization() {
		return fmt.Errorf("RV64 table storage owner lost physical resources")
	}
	table.owner.nativeLeases++
	return nil
}

// Called inside the parked internal type helper. The active entry scope owns
// every physical resource before any candidate native slot is read.
func railshotRetainedFunctionExactType(in *Instance, descriptor uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	lease := railshotCurrentNativeTableTargets(in)
	if lease.targets == nil {
		return ValueTypeDescriptor{}, nil, false
	}
	for _, owner := range lease.targets.owners[:lease.targets.count] {
		if _, valid := rv64PublishedDescriptor(owner, descriptor); valid {
			return instanceFuncrefExactType(owner, descriptor)
		}
	}
	return ValueTypeDescriptor{}, nil, false
}

// Import wrappers retain their own entry/home/context, while ref.func retains
// the native producer's actual dynamic type. The frozen attachment owns that
// producer through every descriptor lookup and call.
func rv64AttachedFunctionTypeKey(in *Instance, index int) uint64 {
	if in == nil || in.c == nil || index < 0 || index >= len(in.c.FuncTypeID) {
		return 0
	}
	if index < in.c.NumImports {
		entries := in.importReferenceEntries()
		if index >= len(entries) {
			return 0
		}
		if target := entries[index].native; target != nil {
			if target.inst == nil || target.inst.c == nil || target.localIdx < 0 || target.localIdx >= len(target.inst.c.Funcs) {
				return 0
			}
			return target.inst.c.funcTypeKey(target.inst.c.NumImports + target.localIdx)
		}
	}
	return in.c.funcTypeKey(index)
}

// Aliased imports share one storage lease, while each (table, writer) pair
// needs its own suspended-writer promise. Owner ordinals are not table ordinals.
func (s *rv64NativeTableTargets) retainImportedTable(table *Table, ordinal int) error {
	owner := s.owners[ordinal]
	foundStorage := false
	for _, existing := range s.storage {
		if existing == table {
			foundStorage = true
			break
		}
	}
	if !foundStorage {
		if len(s.storage) == rv64MaxTableLeaseEdges {
			return fmt.Errorf("RV64 imported table storage edge budget")
		}
		if s.storage == nil {
			s.storage = s.smallStorage[:0]
		}
		// Reserve journal capacity before acquiring physical ownership.
		s.storage = append(s.storage, nil)
		if err := table.acquireRV64NativeStorage(); err != nil {
			s.storage = s.storage[:len(s.storage)-1]
			return err
		}
		s.storage[len(s.storage)-1] = table
	}
	// A declaration-only owner has no callable descriptor that native code
	// can publish. Keep its storage lease without a transient writer root.
	if owner.c.NumImports == 0 && len(owner.c.Funcs) == 0 {
		return nil
	}
	for _, writer := range s.writers {
		if writer.table == table && writer.owner == uint16(ordinal) {
			return nil
		}
	}
	if len(s.writers) == rv64MaxTableLeaseEdges {
		return fmt.Errorf("RV64 imported table writer edge budget")
	}
	if s.writers == nil {
		s.writers = s.smallWriters[:0]
	}
	s.writers = append(s.writers, rv64TableWriter{})
	if err := s.activateWriter(table, owner); err != nil {
		s.writers = s.writers[:len(s.writers)-1]
		return err
	}
	s.writers[len(s.writers)-1] = rv64TableWriter{table: table, owner: uint16(ordinal)}
	return nil
}
