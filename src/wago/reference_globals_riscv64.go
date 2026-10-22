//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type importGlobalReference struct {
	global     *Global
	owner      *Instance
	descriptor uint64
	dependency *rv64FrozenGlobalDependencyRow
}

// The caller holds a normal global import attachment. Snapshot the container
// without nesting its lock with the store or instance lifetime locks.
func rv64ReferenceGlobalSnapshot(global *Global, store *referenceStore, attached bool) (uint64, *Instance, error) {
	if global == nil || global.owner == nil || store == nil {
		return 0, nil, fmt.Errorf("RV64 reference global requires an explicit Runtime owner")
	}
	o := global.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || attached && o.importers == 0 || o.store != store || o.typ != ValFuncRef || o.mutable || global.Type != ValFuncRef || global.Mutable || len(global.cell) < 8 || o.hasValueType && !rv64PrimitiveFunctionGlobalType(o.valueType, o.types) {
		return 0, nil, fmt.Errorf("RV64 reference global requires an attached immutable function reference")
	}
	bits := readGlobalObject(global, ValFuncRef)
	if bits == 0 && o.hasValueType && !o.valueType.Ref.Nullable {
		return 0, nil, fmt.Errorf("RV64 non-null function global contains null")
	}
	return bits, o.instance, nil
}

func rv64PrimitiveFunctionGlobalType(value ValueTypeDescriptor, types []DefinedTypeDescriptor) bool {
	if value.Kind != ValueTypeReference || value.Ref.Exact {
		return false
	}
	heap := value.Ref.Heap
	if heap.Defined {
		return heap.Abstract == 0 && uint64(heap.TypeIndex) < uint64(len(types)) && types[heap.TypeIndex].Kind == CompositeTypeFunction
	}
	return heap.TypeIndex == 0 && (heap.Abstract == AbstractHeapFunc || heap.Abstract == AbstractHeapNoFunc)
}

// Imported immutable cells form construction-time attachment chains. Each
// frozen edge names the arena containing the raw slot, not a different host
// importer's canonical token slot. No process-wide instance search is needed.
func rv64GlobalDescriptorOwner(source *Instance, descriptor uint64) *Instance {
	if source == nil {
		return nil
	}
	if _, own := source.funcrefDescriptorIndex(descriptor); own {
		return source
	}
	for _, entry := range source.importGlobalReferences() {
		if entry.descriptor == descriptor && entry.owner != nil {
			return entry.owner
		}
	}
	return nil
}

// Acquires a physical root while the existing global attachment or store token
// proves the raw slot's lifetime. Release it outside all runtime/container locks.
func rv64RetainGlobalDescriptor(global *Global, store *referenceStore) (importGlobalReference, error) {
	descriptor, source, err := rv64ReferenceGlobalSnapshot(global, store, true)
	entry := importGlobalReference{global: global, descriptor: descriptor}
	if err != nil || descriptor == 0 {
		return entry, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	owner := rv64GlobalDescriptorOwner(source, descriptor)
	if source == nil {
		if token := store.byIdentity[funcrefIdentity{descriptor: descriptor}]; token != nil && token.descriptor == descriptor {
			owner = token.owner
		}
	}
	if owner == nil || owner.refStore != store || !store.hasInstanceResourcesLocked(owner) {
		return entry, fmt.Errorf("RV64 reference global has no retained descriptor owner")
	}
	if _, valid := rv64PublishedDescriptor(owner, descriptor); !valid {
		return entry, fmt.Errorf("RV64 reference global descriptor is not a published function")
	}
	if !owner.retainResourceRootForFinalization() {
		return entry, fmt.Errorf("RV64 reference global descriptor storage is closed")
	}
	entry.owner = owner
	return entry, nil
}

// Frozen global entries share ONE acquired root per distinct owner. Consume
// all aliases before releasing that root: clearing an earlier slot must not
// erase the deduplication witness for a later alias. The bounded nested scan
// preserves first-occurrence order without allocating another owner set.
func railshotReleaseGlobalReferences(entries []importGlobalReference) {
	var cleanup resourceCleanup
	for i := range entries {
		if row := entries[i].dependency; row != nil {
			cleanup.run(func() { row.holder.release(row) })
			continue
		}
		owner := entries[i].owner // Reread after recursive drains of this list.
		entries[i] = importGlobalReference{}
		if owner == nil {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if entries[j].owner == owner {
				entries[j] = importGlobalReference{}
			}
		}
		cleanup.run(owner.releaseResourceRoot)
	}
	cleanup.finish()
}

func railshotFreezeGlobalReferences(c *Compiled, globals []*resolvedGlobalImport, store *referenceStore, cleanup *resourceCleanup) ([]importGlobalReference, error) {
	return rv64FreezeGlobalReferences(c, globals, store, nil, cleanup)
}

func rv64FreezeGlobalReferences(c *Compiled, globals []*resolvedGlobalImport, store *referenceStore, graph *dependencyGraph, cleanup *resourceCleanup) (entries []importGlobalReference, err error) {
	if cleanup.panicked {
		return nil, ErrCallbackPanic
	}
	count := 0
	for _, imp := range c.GlobalImports {
		if imp.Type == ValFuncRef && !imp.Mutable {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	entries = make([]importGlobalReference, 0, count)
	completed := false
	defer func() {
		if !completed {
			// Use the outer constructor's journal: rollback must not replace an
			// established primary error or the first body/cleanup panic.
			cleanup.run(func() { railshotReleaseGlobalReferences(entries) })
			if !railshotHasFrozenGlobalDependencies(entries) {
				entries = nil
			}
		}
	}()
	var holder *rv64FrozenGlobalDependencyRows
	for i, imp := range c.GlobalImports {
		if imp.Type != ValFuncRef || imp.Mutable {
			continue
		}
		global := globals[i].global
		duplicate := false
		for _, entry := range entries {
			duplicate = duplicate || entry.global == global
		}
		if duplicate {
			continue
		}
		entry, err := rv64RetainGlobalDescriptor(global, store)
		if err != nil {
			return entries, err
		}
		var row *rv64FrozenGlobalDependencyRow
		for _, prior := range entries {
			if entry.owner != nil && entry.owner == prior.owner {
				cleanup.run(entry.owner.releaseResourceRoot) // Existing row still pins it.
				row = prior.dependency
				break
			}
		}
		if cleanup.panicked {
			return entries, ErrCallbackPanic
		}
		newOwner := entry.owner != nil && row == nil
		exchangeBootstrap := false
		if newOwner {
			root, exchange, acquireErr := rv64AcquireFrozenGlobalDependency(entry.owner, graph, cleanup)
			if acquireErr != nil {
				return entries, acquireErr
			}
			if holder == nil {
				holder = &rv64FrozenGlobalDependencyRows{}
			}
			row = &rv64FrozenGlobalDependencyRow{holder: holder, root: root}
			holder.rows.push(row)
			exchangeBootstrap = exchange
		}
		entry.dependency = row
		entries = append(entries, entry)
		if holder != nil {
			holder.entries = entries
		}
		if exchangeBootstrap {
			cleanup.run(entry.owner.releaseResourceRoot)
			if cleanup.panicked {
				return entries, ErrCallbackPanic
			}
		}
		if newOwner {
			// A token-created host global need not have exported a GlobalObject.
			// Quiesce its owner before native cross-instance reachability, outside
			// all store, container and native execution locks.
			entry.owner.markNativeControlShared()
		}
	}
	completed = true
	return entries, nil
}

// Preflight runs before executable mapping; ordinary construction repeats the
// check after attaching every import. No tokens are issued during admission.
func rv64ValidateResolvedGlobals(c *Compiled, imports resolvedImports, store *referenceStore) error {
	var attached []*Global
	// Detachment may release a producer's final physical root and invoke
	// cleanup. Keep it outside native serialization, as the table preflight does.
	defer func() {
		for i := len(attached) - 1; i >= 0; i-- {
			attached[i].detachReferenceImporter()
		}
	}()
	for i, imp := range c.GlobalImports {
		if imp.Type != ValFuncRef && !isGCRefValType(imp.Type) {
			continue
		}
		provided, ok := imports.global(c.globalImportBindingKey(i))
		if !ok || provided.Global == nil {
			return fmt.Errorf("RV64 reference import requires an explicit store-bound *Global")
		}
		global := provided.Global
		if err := c.validateImportedGlobal(imp.Module+"."+imp.Name, global, imp); err != nil {
			return err
		}
		var collector *gc.Collector
		if isGCRefValType(imp.Type) {
			if global.owner != nil {
				global.owner.mu.Lock()
				if global.owner.instance != nil {
					collector = global.owner.instance.gc
				}
				global.owner.mu.Unlock()
			}
		}
		var attachErr error
		if isGCRefValType(imp.Type) {
			nativeExecutionMu.Lock()
			attachErr = global.attachReferenceImporterWithCollector(store, collector)
			nativeExecutionMu.Unlock()
		} else {
			attachErr = global.attachReferenceImporterWithCollector(store, collector)
		}
		if attachErr != nil {
			return attachErr
		}
		attached = append(attached, global)
		if imp.Mutable {
			continue
		}
		if isGCRefValType(imp.Type) {
			continue
		}
		entry, err := rv64RetainGlobalDescriptor(global, store)
		if entry.owner != nil {
			entry.owner.releaseResourceRoot()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Called under native serialization and the consumer's invocation lease.
// An immutable imported cell or a local copy authorizes first issuance from a
// logically retired producer. Direct access through that producer stays closed.
func railshotImportedGlobalValue(in *Instance, cell *Global) (Value, bool, error) {
	if in == nil || cell == nil || cell.Type != ValFuncRef || len(in.importGlobalReferences()) == 0 {
		return Value{}, false, nil
	}
	var descriptor uint64
	if cell.owner == nil {
		// A local cell gains its public owner lazily on ExportedGlobalObject.
		// Before that, the invocation lease and frozen directory own its bytes.
		local := false
		for i := len(in.c.GlobalImports); i < len(in.globalCells); i++ {
			if in.globalCells[i] == cell && in.c.Globals[i].Type == ValFuncRef && !in.c.Globals[i].Mutable {
				local = true
				break
			}
		}
		if !local || cell.Mutable || len(cell.cell) < 8 {
			return Value{}, true, fmt.Errorf("RV64 reference global has no local storage owner")
		}
		descriptor = readGlobalObject(cell, ValFuncRef)
	} else {
		var err error
		descriptor, _, err = rv64ReferenceGlobalSnapshot(cell, in.refStore, false)
		if err != nil {
			return Value{}, true, err
		}
	}
	for _, entry := range in.importGlobalReferences() {
		if entry.global != cell && (descriptor == 0 || entry.descriptor != descriptor) {
			continue
		}
		if descriptor != entry.descriptor {
			return Value{}, true, fmt.Errorf("RV64 immutable reference global changed")
		}
		if descriptor == 0 {
			return Value{typ: ValFuncRef}, true, nil
		}
		if _, valid := rv64PublishedDescriptor(entry.owner, descriptor); !valid {
			return Value{}, true, fmt.Errorf("RV64 reference global descriptor changed")
		}
		token, err := in.refStore.issueAttachedResult(entry.owner, descriptor)
		return Value{typ: ValFuncRef, bits: token}, true, err
	}
	return Value{}, false, nil
}

func railshotCanonicalGlobalFuncref(store *referenceStore, source *Instance, descriptor uint64) (*Instance, uint64, bool) {
	owner := rv64GlobalDescriptorOwner(source, descriptor)
	if owner == nil {
		owner = rv64ImportedResultDescriptorOwner(store, source, descriptor)
	}
	if owner == nil && store != nil {
		// A mutable cell can contain a token-issued function from a producer
		// outside its container's frozen import graph. The store identity owns
		// that producer's physical root. The caller holds store.mu.
		if entry := store.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil && entry.descriptor == descriptor {
			owner = entry.owner
		}
	}
	if owner == nil || owner == source || owner.refStore != store || !store.hasInstanceResourcesLocked(owner) {
		return nil, 0, false
	}
	if _, valid := rv64PublishedDescriptor(owner, descriptor); !valid {
		return nil, 0, false
	}
	return store.canonicalFuncrefOwnerLocked(owner, descriptor)
}

// Frozen function attachments retain each reachable producer while the source
// is retained. Establish membership in those owned descriptor arenas before
// reading publication fields. This path also covers a producer's first result
// after logical close, without searching unrelated registered instances.
// The caller holds store.mu and the source's invocation or attachment lease.
func rv64ImportedResultDescriptorOwner(store *referenceStore, source *Instance, descriptor uint64) *Instance {
	if store == nil || source == nil || source.refStore != store || descriptor == 0 {
		return nil
	}
	inline := [8]*Instance{source}
	owners := inline[:1]
	var ownerIndex map[*Instance]struct{}
	edges := 0
	for next := 0; next < len(owners); next++ {
		parent := owners[next]
		for index := 0; index < parent.c.NumImports; index++ {
			edges++
			if edges > rv64MaxTableLeaseEdges {
				return nil
			}
			export := parent.importReferenceNative(index)
			if export == nil {
				// Table-free relays have no descriptor directory. Their sealed
				// resolved binding still owns the same import attachment.
				export, _ = rv64NativeFunctionAttachment(parent, index)
			}
			if export == nil || export.inst == nil {
				continue
			}
			owner := export.inst
			if owner.refStore != store || !store.hasInstanceResourcesLocked(owner) {
				continue
			}
			if _, own := owner.funcrefDescriptorIndex(descriptor); own {
				if _, valid := rv64PublishedDescriptor(owner, descriptor); valid {
					return owner
				}
				return nil
			}
			var ok bool
			owners, ok = rv64AppendOwnerForWalk(owners, &ownerIndex, owner)
			if !ok {
				return nil
			}

		}
	}
	return nil
}
