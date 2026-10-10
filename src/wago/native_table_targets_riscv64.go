//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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
	owners          [257]*Instance
	writers         [257]*Table
	storage         [257]*Table
	count           int
	tailBudget      rv64NativeTailGenerationBudget
	tailGenerations []*rv64NativeTailGeneration // all versions, including suspended code
	tailActive      []*rv64NativeTailGeneration
	tailOwnerCount  int
	tailRetiring    bool
}

func (s *rv64NativeTableTargets) add(owner *Instance) error {
	if owner == nil {
		return fmt.Errorf("RV64 table target owner is nil")
	}
	for _, existing := range s.owners[:s.count] {
		if existing == owner {
			return nil
		}
	}
	if s.count == len(s.owners) {
		return fmt.Errorf("RV64 table target owner bound")
	}
	// Existing table/direct attachments authorize use after logical Close. The
	// container lock or a retained predecessor still owns physical storage here.
	if !owner.retainResourceRootForFinalization() {
		return fmt.Errorf("RV64 table target lost physical resources")
	}
	s.owners[s.count] = owner
	s.count++
	return nil
}

func (s *rv64NativeTableTargets) release() {
	if s.tailBudget != (rv64NativeTailGenerationBudget{}) {
		panic("RV64 tail generations must retire before scope roots")
	}
	var cleanup resourceCleanup
	cleanup.run(s.publishImportedReferenceRows)
	// Remove transient writer promises and scan current rows under serialization;
	// prune performs physical releases only after its own native lease unlocks.
	for index, table := range s.writers {
		if table == nil {
			continue
		}
		table.mu.Lock()
		state := table.retained[s.owners[index]]
		if state == nil || state.activeWriters == 0 {
			panic("RV64 table writer release order")
		}
		state.activeWriters--
		table.mu.Unlock()
		s.writers[index] = nil
		cleanup.run(table.pruneRetainedInstances)
	}
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
	}
	// Releasing the last retired root can reenter finalization/native locking.
	for s.count != 0 {
		s.count--
		owner := s.owners[s.count]
		s.owners[s.count] = nil
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
		return fmt.Errorf("RV64 retained table storage/type is invalid")
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
	if s.count != 0 && s.owners[0] != root {
		return fmt.Errorf("RV64 table target set cannot change its root")
	}
	if err := s.add(root); err != nil {
		return err
	}
	return s.validateReachable()
}

// The caller holds nativeExecutionMu and releases roots only after unlocking.
func (s *rv64NativeTableTargets) validateReachable() error {
	edges := 0
	for cursor := 0; cursor < s.count; cursor++ {
		owner := s.owners[cursor]
		if owner.c == nil || owner.jm == nil || owner.base == 0 || owner.nativeContext == 0 || owner.hostEvents != nil || !owner.c.nativeContextTrapABI {
			return fmt.Errorf("RV64 table target requires a retained native framed owner")
		}
		if _, err := (rv64TailCodeOwner{owner}).nativeOwnerCodeView(true); err != nil {
			return err
		}
		if err := owner.c.validateCached(); err != nil {
			return err
		}
		if owner.c.tableCount() > 1 && !rv64OwnedTwoTable32Size(owner.c) && !rv64OwnedTwoTable32GetSet(owner.c) && !rv64OwnedTwoTable32Grow(owner.c) && !rv64OwnedTwoTable32Fill(owner.c) && !rv64OwnedTwoTable32Copy(owner.c) && !rv64OwnedTwoTable32InitDrop(owner.c) && !rv64OwnedTwoTable64Size(owner.c) && !rv64OwnedTwoTable64GetSet(owner.c) && !rv64OwnedTwoTable64Grow(owner.c) && !rv64OwnedTwoTable64Fill(owner.c) && !rv64OwnedTwoTable64Copy(owner.c) && !rv64OwnedTwoTable64InitDrop(owner.c) && !rv64OwnedTwoTable64Indirect(owner.c) {
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
			table, ok := owner.imports.table(owner.c.tableImportBindingKey(index))
			if !ok {
				return fmt.Errorf("RV64 retained imported table is missing")
			}
			if err := s.tableRoots(table, &edges); err != nil {
				return err
			}
			if s.storage[cursor] == nil {
				if err := table.acquireRV64NativeStorage(); err != nil {
					return err
				}
				s.storage[cursor] = table
			}
			if s.writers[cursor] == nil {
				if err := s.activateWriter(table, owner); err != nil {
					return err
				}
				s.writers[cursor] = table
			}
		}
		owner.lifeMu.Lock()
		table := owner.table
		owner.lifeMu.Unlock()
		for table != nil {
			edges++
			if edges > 4096 {
				return fmt.Errorf("RV64 table attachment edge bound")
			}
			if err := s.tableRoots(table, &edges); err != nil {
				return err
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
		for tableIndex := 0; tableIndex < owner.c.tableCount(); tableIndex++ {
			if err := s.validateTable(owner.tableDescriptor(tableIndex)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) validateTable(table []byte) error {
	if len(table) < 8 {
		return fmt.Errorf("RV64 table descriptor extent")
	}
	size, capacity := binary.LittleEndian.Uint32(table), binary.LittleEndian.Uint32(table[4:])
	if size > capacity || capacity > railshot.MaxOwnedTableEntries || uint64(8)+uint64(capacity)*coreruntime.TableEntryBytes > uint64(len(table)) {
		return fmt.Errorf("RV64 table descriptor capacity/extent")
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
	canonical := owner.funcRefDescs[offset : offset+coreruntime.FuncRefDescBytes]
	valid := binary.LittleEndian.Uint64(canonical) == entry &&
		binary.LittleEndian.Uint64(canonical[coreruntime.TableEntrySigKeyOffset:]) == owner.c.FuncTypeID[function] &&
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
		for _, reference := range references {
			if reference.owner == nil {
				continue
			}
			duplicate := false
			for _, owner := range candidates.owners[:candidates.count] {
				duplicate = duplicate || owner == reference.owner
			}
			if !duplicate {
				// Admission caps the complete imported/local global space at 256.
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
