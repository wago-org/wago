package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// importDedup is an insertion-ordered set of distinct comparable values — the
// engine uses it for import owner pointers (host funcrefs, reference globals,
// imported tables). A small inline array keeps the common case (a handful of
// imports) allocation-free; overflow spills to a slice. The zero value is ready
// to use.
type importDedup[T comparable] struct {
	inline [4]T
	n      int
	extra  []T
}

func (d *importDedup[T]) contains(v T) bool {
	for i := 0; i < d.n && i < len(d.inline); i++ {
		if d.inline[i] == v {
			return true
		}
	}
	for _, e := range d.extra {
		if e == v {
			return true
		}
	}
	return false
}

// push appends v unconditionally; callers needing dedup use add or guard with
// contains first.
func (d *importDedup[T]) push(v T) {
	if d.n < len(d.inline) {
		d.inline[d.n] = v
	} else {
		d.extra = append(d.extra, v)
	}
	d.n++
}

// add inserts v if absent and reports whether it was newly inserted.
func (d *importDedup[T]) add(v T) bool {
	if d.contains(v) {
		return false
	}
	d.push(v)
	return true
}

// each calls fn for every distinct element in insertion order.
func (d *importDedup[T]) each(fn func(T)) {
	inlineCount := d.n
	if inlineCount > len(d.inline) {
		inlineCount = len(d.inline)
	}
	for i := 0; i < inlineCount; i++ {
		fn(d.inline[i])
	}
	for _, e := range d.extra {
		fn(e)
	}
}

// reset empties the set, clearing the inline array so it retains no references.
func (d *importDedup[T]) reset() {
	inlineCount := d.n
	if inlineCount > len(d.inline) {
		inlineCount = len(d.inline)
	}
	var zero T
	for i := 0; i < inlineCount; i++ {
		d.inline[i] = zero
	}
	d.n = 0
	d.extra = nil
}

type functionImportAttachments struct {
	set importDedup[*Instance]
}

func (a *functionImportAttachments) attach(export *InstanceExport) error {
	if export == nil || export.inst == nil {
		return fmt.Errorf("instance export is nil")
	}
	producer := export.inst
	if a.set.contains(producer) {
		return nil
	}
	if !producer.retainResourceRoot() {
		return fmt.Errorf("producer instance is closed")
	}
	a.set.push(producer)
	return nil
}

func (a *functionImportAttachments) detachAll() {
	a.set.each((*Instance).releaseResourceRoot)
	a.set.reset()
}

func detachImportedFunctions(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	var seen importDedup[*Instance]
	for _, key := range in.c.Imports {
		export, ok := in.imports[key].(*InstanceExport)
		if !ok || export == nil || export.inst == nil {
			continue
		}
		if seen.add(export.inst) {
			export.inst.releaseResourceRoot()
		}
	}
}

type hostFuncRefAttachments struct {
	set      importDedup[*HostFuncRef]
	bindings importDedup[hostFuncRefBindingKey]
}

func (a *hostFuncRefAttachments) attach(owner *HostFuncRef, store *referenceStore, sig FuncSig, collector *gc.Collector, domainID uint64, c *Compiled, importIndex int) error {
	if owner == nil {
		return fmt.Errorf("host funcref owner is nil")
	}
	newOwner := !a.set.contains(owner)
	if newOwner {
		if err := owner.attachImporter(store, sig, collector, domainID, c); err != nil {
			return err
		}
	} else if err := owner.validateAttachedImporter(store, sig, collector, domainID, c); err != nil {
		return err
	}
	_, exact, err := owner.acquireDispatchBinding(store, c, importIndex, sig)
	if err != nil {
		if newOwner {
			owner.detachImporter()
		}
		return err
	}
	if exact {
		a.bindings.push(hostFuncRefBindingKey{owner: owner, compiled: c, importIndex: importIndex})
	}
	if newOwner {
		a.set.push(owner)
	}
	return nil
}

func (a *hostFuncRefAttachments) detachAll() {
	a.bindings.each(func(key hostFuncRefBindingKey) { key.owner.releaseDispatchBinding(key.compiled, key.importIndex) })
	a.bindings.reset()
	a.set.each((*HostFuncRef).detachImporter)
	a.set.reset()
}

func detachImportedHostFuncRefs(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	var seen importDedup[*HostFuncRef]
	for i, key := range in.c.Imports {
		owner, ok := in.imports[key].(*HostFuncRef)
		if !ok || owner == nil {
			continue
		}
		owner.releaseDispatchBinding(in.c, i)
		if seen.add(owner) {
			owner.detachImporter()
		}
	}
}

type globalImportAttachments struct {
	set importDedup[*Global]
}

func (a *globalImportAttachments) attach(global *Global, store *referenceStore, collector *gc.Collector) error {
	if global == nil {
		return fmt.Errorf("global is nil")
	}
	validate := global.validateNumericImport
	attach := global.attachNumericImporter
	if isReferenceValType(global.Type) {
		validate = func() error { return global.validateReferenceImportWithCollector(store, collector) }
		attach = func() error { return global.attachReferenceImporterWithCollector(store, collector) }
	}
	if a.set.contains(global) {
		return validate()
	}
	if err := attach(); err != nil {
		return err
	}
	a.set.push(global)
	return nil
}

func (a *globalImportAttachments) detachAll() {
	a.set.each((*Global).detachReferenceImporter)
	a.set.reset()
}

func detachImportedGlobals(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	var seen importDedup[*Global]
	for _, imp := range in.c.GlobalImports {
		provided, ok := in.imports.global(imp.Module + "." + imp.Name)
		if !ok || provided.Global == nil || (!isReferenceValType(imp.Type) && provided.Global.owner == nil) {
			continue
		}
		if seen.add(provided.Global) && !in.ownsTransferredGlobalAttachment(provided.Global) {
			provided.Global.detachReferenceImporter()
		}
	}
}

func retainProducerRootsInImportedGlobals(in *Instance) bool {
	return retainProducerRootsInImportedGlobalsMode(in, false)
}

func retainProducerRootsInImportedGlobalsForFinalization(in *Instance) bool {
	return retainProducerRootsInImportedGlobalsMode(in, true)
}

func retainProducerRootsInImportedGlobalsMode(in *Instance, finalization bool) bool {
	if in == nil || in.c == nil {
		return false
	}
	retained := false
	var seen importDedup[*Global]
	for _, imp := range in.c.GlobalImports {
		if imp.Type != ValFuncRef {
			continue
		}
		provided, ok := in.imports.global(imp.Module + "." + imp.Name)
		if !ok || provided.Global == nil {
			continue
		}
		if !seen.add(provided.Global) {
			continue
		}
		var rooted bool
		if finalization {
			rooted = provided.Global.retainProducerInstanceForFinalization(in)
		} else {
			rooted = provided.Global.retainProducerInstance(in)
		}
		if finalization {
			result := provided.Global.retainDescriptorOwnerForFinalization(in.refStore, in)
			if result.retained {
				rooted = true
			}
		}
		for _, producer := range importedFuncrefProducerRoots(in) {
			if finalization {
				if provided.Global.retainProducerInstanceForFinalization(producer) {
					rooted = true
				}
			} else if provided.Global.retainProducerInstance(producer) {
				rooted = true
			}
		}
		if rooted {
			in.transferImportedGlobalAttachment(provided.Global)
			retained = true
		}
	}
	return retained
}

type tableImportAttachments struct {
	set   importDedup[*Table]
	store *referenceStore
}

func (a *tableImportAttachments) attach(table *Table, elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, collector *gc.Collector, addr64 bool) error {
	if err := table.validateImportWithCollector(elementType, exact, types, store, collector, addr64); err != nil {
		return err
	}
	if a.set.contains(table) {
		return nil
	}
	if err := table.attachImporterWithCollector(elementType, exact, types, store, collector, addr64); err != nil {
		return err
	}
	if a.set.n == 0 {
		a.store = store
	} else if a.store != store {
		return fmt.Errorf("table import attachments use inconsistent reference stores")
	}
	a.set.push(table)
	return nil
}

func (a *tableImportAttachments) detachAll() {
	a.set.each(func(table *Table) { table.detachImporter(a.store) })
	a.set.reset()
	a.store = nil
}

func (c *Compiled) preflightImportBindings(imports Imports) error {
	// Function bindings keep their signature-specific validation in
	// validateImportBindings. Storage imports are otherwise resolved in separate
	// setup phases, so verify their presence before attaching or mutating owners.
	for i := range c.GlobalImports {
		imp := c.GlobalImports[i]
		key := imp.Module + "." + imp.Name
		if _, ok := imports[key]; !ok {
			return fmt.Errorf("missing imported global %q", key)
		}
	}
	for i := 0; i < c.memoryImportCount(); i++ {
		def, _ := c.memoryImportAt(i)
		if _, ok := imports[def.ImportKey]; !ok {
			return fmt.Errorf("missing imported memory %q", def.ImportKey)
		}
	}
	for i := 0; i < c.tableImportCount(); i++ {
		def, _ := c.tableImportAt(i)
		if _, ok := imports[def.Key]; !ok {
			return fmt.Errorf("missing imported table %q", def.Key)
		}
	}
	return nil
}

func detachImportedTables(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	var seen importDedup[*Table]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		table, ok := in.imports.table(def.Key)
		if !ok || table == nil {
			continue
		}
		if seen.add(table) && !in.ownsTransferredTableAttachment(table) {
			table.detachImporter(in.refStore)
		}
	}
}

func retainProducerRootsInImportedTables(in *Instance) bool {
	return retainProducerRootsInImportedTablesMode(in, false)
}

func retainProducerRootsInImportedTablesForFinalization(in *Instance) bool {
	return retainProducerRootsInImportedTablesMode(in, true)
}

func retainProducerRootsInImportedTablesMode(in *Instance, finalization bool) bool {
	if in == nil || in.c == nil {
		return false
	}
	retained := false
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		table, ok := in.imports.table(def.Key)
		if !ok || table == nil {
			continue
		}
		var rooted bool
		if finalization {
			rooted = table.retainProducerInstanceForFinalization(in)
		} else {
			rooted = table.retainProducerInstance(in)
		}
		if finalization {
			result := table.retainDescriptorOwnersForFinalization(in.refStore, in)
			if result.retained {
				rooted = true
			}
		}
		// A descriptor copied from another imported table/global or admitted as
		// a public token need not occur in the writer's own funcRefDescs. Carry
		// forward every source container's actual producer roots, while the store
		// resolver above covers still-live token and canonical descriptor owners.
		for _, producer := range importedFuncrefProducerRoots(in) {
			if finalization {
				if table.retainProducerInstanceForFinalization(producer) {
					rooted = true
				}
			} else if table.retainProducerInstance(producer) {
				rooted = true
			}
		}
		if rooted {
			in.transferImportedTableAttachment(table)
			in.transferImportedAttachmentsFromOwner(table.instanceOwner())
			retained = true
		}
	}
	return retained
}

// transferImportedAttachmentsFromOwner breaks a closed consumer/owner cycle
// after one of the owner's tables has taken over the consumer's lifetime. The
// table keeps the consumer callable while any open importer keeps the owner
// live. If no importer remains, closing the owner can release the table and in
// turn release the consumer.
func (in *Instance) transferImportedAttachmentsFromOwner(owner *Instance) {
	if in == nil || in.c == nil || owner == nil {
		return
	}
	var memories importDedup[*Memory]
	for memoryIndex := 0; memoryIndex < in.c.memoryCount(); memoryIndex++ {
		def := in.c.memoryDef(memoryIndex)
		if def.ImportKey == "" {
			continue
		}
		var memory *Memory
		if memoryIndex == 0 {
			memory = in.memory
		} else if in.memoryDir != nil && memoryIndex < len(in.memoryDir.memories) {
			memory = in.memoryDir.memories[memoryIndex]
		}
		if memory != nil && memories.add(memory) && memory.instanceOwner() == owner {
			in.transferImportedMemoryAttachment(memory)
		}
	}

	var globals importDedup[*Global]
	for _, imp := range in.c.GlobalImports {
		provided, ok := in.imports.global(imp.Module + "." + imp.Name)
		if ok && provided.Global != nil && globals.add(provided.Global) && provided.Global.instanceOwner() == owner {
			in.transferImportedGlobalAttachment(provided.Global)
		}
	}

	var tables importDedup[*Table]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		table, ok := in.imports.table(def.Key)
		if ok && table != nil && tables.add(table) && table.instanceOwner() == owner {
			in.transferImportedTableAttachment(table)
		}
	}
}

// importedFuncrefProducerRoots snapshots roots from every imported persistent
// funcref container. Container locks are released before callers attempt any
// destination retention, preserving the order container -> snapshot, then
// referenceStore -> instance, then destination container.
func importedFuncrefProducerRoots(in *Instance) []*Instance {
	if in == nil || in.c == nil {
		return nil
	}
	var roots []*Instance
	seen := make(map[*Instance]struct{})
	add := func(candidates []*Instance) {
		for _, root := range candidates {
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
	var tables importDedup[*Table]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		table, ok := in.imports.table(def.Key)
		if ok && table != nil && tables.add(table) {
			add(table.funcrefProducerRoots())
		}
	}
	var globals importDedup[*Global]
	for globalIndex, imp := range in.c.GlobalImports {
		if imp.Type != ValFuncRef || globalIndex >= len(in.globalCells) {
			continue
		}
		global := in.globalCells[globalIndex]
		if global != nil && globals.add(global) {
			add(global.funcrefProducerRoots())
		}
	}
	return roots
}

func (in *Instance) importsFuncrefStorage() bool {
	if in == nil || in.c == nil {
		return false
	}
	for _, imp := range in.c.GlobalImports {
		if imp.Type == ValFuncRef {
			return true
		}
	}
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		if def.Type == ValFuncRef {
			return true
		}
	}
	return false
}

// reconcileFuncrefRoots drops producer roots after a completed guest invocation
// overwrites the last descriptor held by an imported table/global or by one of
// this instance's exported local tables. The scans are bounded by the declared
// containers' capacities and run only over owners that currently retain closed
// producers.
func (in *Instance) reconcileFuncrefRoots() {
	if in == nil || in.c == nil {
		return
	}
	var globals importDedup[*Global]
	for _, imp := range in.c.GlobalImports {
		if imp.Type != ValFuncRef {
			continue
		}
		provided, ok := in.imports.global(imp.Module + "." + imp.Name)
		if ok && provided.Global != nil && globals.add(provided.Global) {
			provided.Global.pruneRetainedInstances()
		}
	}
	var tables importDedup[*Table]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		table, ok := in.imports.table(def.Key)
		if ok && table != nil && tables.add(table) {
			table.pruneRetainedInstances()
		}
	}
	// Walk the local export-handle chain one link at a time under lifeMu, but
	// reconcile only after releasing it. pruneRetainedInstances may drop a
	// producer's final root and synchronously finalize that producer; its scan can
	// retain this instance as a proxy, so holding lifeMu would invert the lock
	// order. The chain remains physically owned for this active invocation, and
	// per-link locking keeps concurrent lazy next-link publication race-free
	// without allocating a snapshot on the call path.
	in.lifeMu.Lock()
	table := in.table
	in.lifeMu.Unlock()
	for table != nil {
		if table.owner != nil && table.owner.elementType == ValFuncRef && tables.add(table) {
			table.pruneRetainedInstances()
		}
		in.lifeMu.Lock()
		table = table.next
		in.lifeMu.Unlock()
	}
}

func (in *Instance) tableDescriptor(index int) []byte {
	if in == nil || in.c == nil || index < 0 || index >= in.c.tableCount() {
		return nil
	}
	if importDef, imported := in.c.tableImportAt(index); imported {
		table, ok := in.imports.table(importDef.Key)
		if !ok || len(table.desc) < 8 {
			return nil
		}
		return table.desc
	}
	if index == 0 {
		if in.tableDescPtr == 0 || in.tableDescLen <= 0 {
			return nil
		}
		return unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
	}
	dirPtr := in.jm.TableDirPtr()
	if dirPtr == 0 {
		return nil
	}
	dir := unsafe.Slice((*byte)(offHeapPtr(dirPtr)), 8*in.c.tableCount())
	descPtr := uintptr(binary.LittleEndian.Uint64(dir[index*8:]))
	if descPtr == 0 {
		return nil
	}
	capacity := in.c.tableRuntimeCapacity(index)
	return unsafe.Slice((*byte)(offHeapPtr(descPtr)), 8+capacity*in.c.tableEntryBytes(index))
}

// Imports returns a caller-owned snapshot of the imports this instance was
// created with, for retrieving imported objects (e.g. a *Memory or *Global) by
// "module.name" key. Mutating the map does not affect the instance.
func (in *Instance) Imports() Imports {
	if in.imports == nil {
		return nil
	}
	imports := make(Imports, len(in.imports))
	for key, value := range in.imports {
		imports[key] = value
	}
	return imports
}
