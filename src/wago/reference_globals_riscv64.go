//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "fmt"

type importGlobalReference struct {
	global     *Global
	owner      *Instance
	descriptor uint64
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
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	if o.closed || attached && o.importers == 0 || o.store != store || o.typ != ValFuncRef || o.mutable || global.Type != ValFuncRef || global.Mutable || len(global.cell) < 8 || o.hasValueType && o.valueType != plain {
		return 0, nil, fmt.Errorf("RV64 reference global requires an attached immutable nullable funcref")
	}
	return readGlobalObject(global, ValFuncRef), o.instance, nil
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

func railshotReleaseGlobalReferences(entries []importGlobalReference) {
	for i, entry := range entries {
		if entry.owner == nil {
			continue
		}
		duplicate := false
		for _, prior := range entries[:i] {
			duplicate = duplicate || prior.owner == entry.owner
		}
		if !duplicate {
			entry.owner.releaseResourceRoot()
		}
	}
}

func railshotFreezeGlobalReferences(c *Compiled, globals []*resolvedGlobalImport, store *referenceStore) (entries []importGlobalReference, err error) {
	count := 0
	for _, imp := range c.GlobalImports {
		if imp.Type == ValFuncRef {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	entries = make([]importGlobalReference, 0, count)
	defer func() {
		if err != nil {
			railshotReleaseGlobalReferences(entries)
			entries = nil
		}
	}()
	for i, imp := range c.GlobalImports {
		if imp.Type != ValFuncRef {
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
		shared := false
		for _, prior := range entries {
			if entry.owner != nil && entry.owner == prior.owner {
				entry.owner.releaseResourceRoot() // Existing entry still pins it.
				shared = true
				break
			}
		}
		if entry.owner != nil && !shared {
			// A token-created host global need not have exported a GlobalObject.
			// Quiesce its owner before native cross-instance reachability, outside
			// all store, container and native execution locks.
			entry.owner.markNativeControlShared()
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Preflight runs before executable mapping; ordinary construction repeats the
// check after attaching every import. No tokens are issued during admission.
func rv64ValidateResolvedGlobals(c *Compiled, imports resolvedImports, store *referenceStore) error {
	for i, imp := range c.GlobalImports {
		if imp.Type != ValFuncRef {
			continue
		}
		provided, ok := imports.global(c.globalImportBindingKey(i))
		if !ok || provided.Global == nil {
			return fmt.Errorf("RV64 reference import requires an explicit global")
		}
		global := provided.Global
		if err := c.validateImportedGlobal(imp.Module+"."+imp.Name, global, imp); err != nil {
			return err
		}
		if err := global.attachReferenceImporter(store); err != nil {
			return err
		}
		entry, err := rv64RetainGlobalDescriptor(global, store)
		if entry.owner != nil {
			entry.owner.releaseResourceRoot()
		}
		global.detachReferenceImporter()
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
	if owner == nil || owner == source || owner.refStore != store || !store.hasInstanceResourcesLocked(owner) {
		return nil, 0, false
	}
	if _, valid := rv64PublishedDescriptor(owner, descriptor); !valid {
		return nil, 0, false
	}
	return store.canonicalFuncrefOwnerLocked(owner, descriptor)
}
