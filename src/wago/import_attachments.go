package wago

import (
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
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
	index  map[T]struct{}
}

func (d *importDedup[T]) contains(v T) bool {
	if d.index != nil {
		_, ok := d.index[v]
		return ok
	}
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
	if d.index != nil {
		d.index[v] = struct{}{}
	} else if d.n > 8 {
		// Keep tiny sets allocation-free. Once the ordered slice has grown past
		// the small case, an index prevents all subsequent membership checks from
		// rescanning the accumulated owners.
		d.index = make(map[T]struct{}, d.n)
		d.each(func(item T) { d.index[item] = struct{}{} })
	}
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

// drain consumes this batch before invoking its release actions, so reentry
// cannot release the same unit again. Each action must consume its ownership
// unit before any callback that can panic (as releaseResourceRoot and importer
// detach do). Callers own serialization; this is not a concurrent journal.
// Entries added by reentry form a separate batch and are left for its owner.
func (d *importDedup[T]) drain(fn func(T)) {
	pending := *d
	*d = importDedup[T]{}
	pending.index = nil
	var cleanup resourceCleanup
	var zero T
	inlineCount := pending.n
	if inlineCount > len(pending.inline) {
		inlineCount = len(pending.inline)
	}
	for i := 0; i < inlineCount; i++ {
		item := pending.inline[i]
		pending.inline[i] = zero
		cleanup.run(func() { fn(item) })
	}
	for i := range pending.extra {
		item := pending.extra[i]
		pending.extra[i] = zero
		cleanup.run(func() { fn(item) })
	}
	cleanup.finish()
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
	d.index = nil
}

type functionImportAttachments struct {
	dependencies functionDependencyAttachments // empty on existing targets; not trailing
	set          importDedup[*Instance]
}

type memoryImportAttachments struct {
	dependencies memoryDependencyAttachments // empty on existing targets; not trailing
	set          importDedup[*Memory]
}

func (a *memoryImportAttachments) attach(memory *Memory, cleanup *resourceCleanup) error {
	if a.set.contains(memory) {
		return nil
	}
	if err := railshotAcquireMemoryDependency(a, memory, cleanup); err != nil {
		return err
	}
	a.set.push(memory)
	return nil
}

func (a *memoryImportAttachments) detachAll() {
	if railshotDrainBuilderMemoryDependencies(a) {
		return
	}
	a.set.drain((*Memory).detachImporter)
}

func (a *functionImportAttachments) attach(export *InstanceExport) error {
	if export == nil || export.inst == nil {
		return fmt.Errorf("instance export is nil")
	}
	producer := export.inst
	if a.set.contains(producer) {
		return nil
	}
	if err := railshotAcquireFunctionDependency(a, producer); err != nil {
		return err
	}
	a.set.push(producer)
	return nil
}

func (a *functionImportAttachments) detachAll() {
	if railshotDrainBuilderFunctionDependencies(a) {
		return
	}
	a.set.drain((*Instance).releaseResourceRoot)
}

func detachImportedFunctions(in *Instance) {
	if railshotDrainInstanceFunctionDependencies(in) {
		return
	}
	if in == nil || in.c == nil {
		return
	}
	var cleanup resourceCleanup
	var seen importDedup[*Instance]
	for i := range in.c.Imports {
		export, ok := in.imports[in.c.functionImportBindingKey(i)].(*InstanceExport)
		if !ok || export == nil || export.inst == nil {
			continue
		}
		if seen.add(export.inst) {
			cleanup.run(func() { export.inst.releaseResourceRoot() })
		}
	}
	cleanup.finish()
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
	var cleanup resourceCleanup
	cleanup.run(func() {
		a.bindings.drain(func(key hostFuncRefBindingKey) { key.owner.releaseDispatchBinding(key.compiled, key.importIndex) })
	})
	cleanup.run(func() { a.set.drain((*HostFuncRef).detachImporter) })
	cleanup.finish()
}

func detachImportedHostFuncRefs(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	var cleanup resourceCleanup
	var seen importDedup[*HostFuncRef]
	for i := range in.c.Imports {
		owner, ok := in.imports[in.c.functionImportBindingKey(i)].(*HostFuncRef)
		if !ok || owner == nil {
			continue
		}
		cleanup.run(func() { owner.releaseDispatchBinding(in.c, i) })
		if seen.add(owner) {
			cleanup.run(func() { owner.detachImporter() })
		}
	}
	cleanup.finish()
}

type globalImportAttachments struct {
	dependencies globalDependencyAttachments // empty on existing targets; not trailing
	set          importDedup[*Global]
}

func (a *globalImportAttachments) attach(global *Global, store *referenceStore, collector *gc.Collector, cleanup *resourceCleanup) error {
	if global == nil {
		return fmt.Errorf("global is nil")
	}
	validate := global.validateNumericImport
	if isReferenceValType(global.Type) {
		validate = func() error { return global.validateReferenceImportWithCollector(store, collector) }
	}
	if a.set.contains(global) {
		return validate()
	}
	if err := railshotAcquireGlobalDependency(a, global, store, collector, cleanup); err != nil {
		return err
	}
	a.set.push(global)
	return nil
}

func (a *globalImportAttachments) detachAll() {
	if railshotDrainBuilderGlobalDependencies(a) {
		return
	}
	a.set.drain((*Global).detachReferenceImporter)
}

func detachImportedGlobals(in *Instance) {
	if railshotDrainInstanceGlobalDependencies(in) {
		return
	}
	if in == nil || in.c == nil {
		return
	}
	var cleanup resourceCleanup
	var seen importDedup[*Global]
	for i, imp := range in.c.GlobalImports {
		provided, ok := in.imports.global(in.c.globalImportBindingKey(i))
		if !ok || provided.Global == nil || (!isReferenceValType(imp.Type) && provided.Global.owner == nil) {
			continue
		}
		if seen.add(provided.Global) && !in.ownsTransferredGlobalAttachment(provided.Global) {
			cleanup.run(func() { provided.Global.detachReferenceImporter() })
		}
	}
	cleanup.finish()
}

func retainProducerRootsInImportedGlobals(in *Instance) bool {
	return retainProducerRootsInImportedGlobalsMode(in, false, nil)
}

func retainProducerRootsInImportedGlobalsMode(in *Instance, finalization bool, pass *importedContainerFinalizationPass) bool {
	if in == nil || in.c == nil {
		return false
	}
	var cleanup resourceCleanup
	retained := false
	var seen importDedup[*Global]
	// The imported source roots need one snapshot for this retention pass.
	var roots []*Instance
	rootsLoaded := false
	for i, imp := range in.c.GlobalImports {
		if imp.Type != ValFuncRef {
			continue
		}
		// RV64 immutable imports cannot receive consumer-produced references.
		// Their frozen roots and ordinary cell attachments last until teardown.
		if railshotNativeContextTrapABI() && !imp.Mutable && len(in.importGlobalReferences()) != 0 {
			continue
		}
		provided, ok := in.imports.global(in.c.globalImportBindingKey(i))
		if !ok || provided.Global == nil {
			continue
		}
		if !seen.add(provided.Global) {
			continue
		}
		rooted := provided.Global.retainProducerInstanceModeWithCleanup(in, finalization, &cleanup)
		if finalization {
			result := provided.Global.retainDescriptorOwnerForFinalizationWithCleanup(in.refStore, in, &cleanup)
			rooted = rooted || result.retained
		}
		if !rootsLoaded {
			if pass != nil {
				roots = pass.roots
			} else {
				roots = importedFuncrefProducerRoots(in)
			}
			rootsLoaded = true
		}
		for _, producer := range roots {
			if provided.Global.retainProducerInstanceModeWithCleanup(producer, finalization, &cleanup) {
				rooted = true
			}
		}
		if rooted {
			if pass != nil {
				pass.globals.add(provided.Global)
			} else {
				cleanup.run(func() { in.transferImportedGlobalAttachment(provided.Global) })
			}
			retained = true
		}
	}
	cleanup.finish()
	return retained
}

type tableImportAttachments struct {
	dependencies tableDependencyAttachments // empty on existing targets; not trailing
	set          importDedup[*Table]
	store        *referenceStore
}

func (a *tableImportAttachments) attach(table *Table, elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, collector *gc.Collector, addr64 bool) error {
	if a.set.n != 0 && a.store != store {
		return fmt.Errorf("table import attachments use inconsistent reference stores")
	}
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
	}
	a.set.push(table)
	return nil
}

func (a *tableImportAttachments) detachAll() {
	if railshotDrainBuilderTableDependencies(a) {
		return
	}
	store := a.store
	a.store = nil
	a.set.drain(func(table *Table) { table.detachImporter(store) })
}

func (c *Compiled) preflightImportBindings(imports resolvedImports) error {
	// Function bindings keep their signature-specific validation in
	// validateImportBindings. Storage imports are otherwise resolved in separate
	// setup phases, so verify their presence before attaching or mutating owners.
	for i := range c.GlobalImports {
		imp := c.GlobalImports[i]
		key := importBindingMapKey(imp.Module, imp.Name)
		if _, ok := imports[key]; !ok {
			return fmt.Errorf("missing imported global %q", imp.Module+"."+imp.Name)
		}
	}
	for i := 0; i < c.memoryImportCount(); i++ {
		def, _ := c.memoryImportAt(i)
		if _, ok := imports[c.memoryImportBindingKey(i)]; !ok {
			return fmt.Errorf("missing imported memory %q", def.ImportKey)
		}
	}
	for i := 0; i < c.tableImportCount(); i++ {
		def, _ := c.tableImportAt(i)
		if _, ok := imports[c.tableImportBindingKey(i)]; !ok {
			return fmt.Errorf("missing imported table %q", def.Key)
		}
	}
	return nil
}

func detachImportedTables(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	if railshotDrainInstanceTableDependencies(in) {
		return
	}
	var cleanup resourceCleanup
	var seen importDedup[*Table]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		table, ok := in.imports.table(in.c.tableImportBindingKey(tableIndex))
		if !ok || table == nil {
			continue
		}
		if seen.add(table) && !in.ownsTransferredTableAttachment(table) {
			cleanup.run(func() { table.detachImporter(in.refStore) })
		}
	}
	cleanup.finish()
}

func retainProducerRootsInImportedTables(in *Instance) bool {
	return retainProducerRootsInImportedTablesMode(in, false, nil)
}

func retainProducerRootsInImportedTablesMode(in *Instance, finalization bool, pass *importedContainerFinalizationPass) bool {
	if in == nil || in.c == nil {
		return false
	}
	var cleanup resourceCleanup
	retained := false
	var seen importDedup[*Table]
	// Each table handles the writer directly; source roots can be reused.
	var roots []*Instance
	rootsLoaded := false
	var transferredOwners importDedup[*Instance]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		table, ok := in.imports.table(in.c.tableImportBindingKey(tableIndex))
		if !ok || table == nil {
			continue
		}
		// Finalization transfers each imported table only once. The ordinary
		// retention pass keeps its small-import and large-table scan costs.
		if finalization && !seen.add(table) {
			continue
		}
		rooted := table.retainProducerInstanceModeWithCleanup(in, finalization, &cleanup)
		if finalization {
			result := table.retainDescriptorOwnersForFinalizationWithCleanup(in.refStore, in, &cleanup)
			rooted = rooted || result.retained
		}
		// A descriptor copied from another imported table/global or admitted as
		// a public token need not occur in the writer's own funcRefDescs. Carry
		// forward every source container's actual producer roots, while the store
		// resolver above covers still-live token and canonical descriptor owners.
		if !rootsLoaded {
			if pass != nil {
				roots = pass.roots
			} else {
				roots = importedFuncrefProducerRoots(in)
			}
			rootsLoaded = true
		}
		for _, producer := range roots {
			if table.retainProducerInstanceModeWithCleanup(producer, finalization, &cleanup) {
				rooted = true
			}
		}
		if rooted {
			if pass != nil {
				pass.tables.add(table)
				if owner := table.instanceOwner(); owner != nil {
					pass.owners.add(owner)
				}
			} else {
				cleanup.run(func() { in.transferImportedTableAttachment(table) })
				if owner := table.instanceOwner(); owner != nil && transferredOwners.add(owner) {
					cleanup.run(func() { in.transferImportedAttachmentsFromOwner(owner) })
				}
			}
			retained = true
		}
	}
	cleanup.finish()
	return retained
}

// Finalization retains all destinations before any import attachment is handed
// over. Until transfer, the importer pins each source container and its roots.
type importedContainerFinalizationPass struct {
	roots   []*Instance
	tables  importDedup[*Table]
	globals importDedup[*Global]
	owners  importDedup[*Instance]
}

// Discover descriptor owners in every imported funcref container before any
// destination transfers an import attachment. A root first resolved in a later
// table or global may be the only producer for an earlier unresolved descriptor.
// The retention passes then share one source-root snapshot. Transfers occur only
// after every destination has retained its roots, so concurrent closes cannot
// release a producer between destinations.
func retainProducerRootsInImportedContainersForFinalization(in *Instance) bool {
	return retainProducerRootsInImportedContainersForFinalizationWithBeforeTransfer(in, nil)
}

// beforeTransfer is a test seam for checking that imported containers remain
// pinned after retention and before the deferred attachment handover.
func retainProducerRootsInImportedContainersForFinalizationWithBeforeTransfer(in *Instance, beforeTransfer func()) bool {
	if in == nil || in.c == nil {
		return false
	}
	var cleanup resourceCleanup
	// RV64 imports carry exact table and frozen-global dependency receipts. Those
	// receipts are transferred by each container pass and deliberately exclude
	// immutable frozen cells from generic producer retention. A generic discovery
	// pass would reacquire the retiring writer through those raw descriptor views,
	// leaving construction rollback and terminal close permanently rooted.
	if railshotNativeContextTrapABI() {
		var retainedTables, retainedGlobals bool
		cleanup.run(func() { retainedTables = retainProducerRootsInImportedTablesMode(in, true, nil) })
		cleanup.run(func() { retainedGlobals = retainProducerRootsInImportedGlobalsMode(in, true, nil) })
		if beforeTransfer != nil {
			cleanup.run(beforeTransfer)
		}
		cleanup.finish()
		return retainedTables || retainedGlobals
	}
	// One imported container has no other destination to propagate back to.
	if in.c.tableImportCount()+len(in.c.GlobalImports) > 1 {
		var tables importDedup[*Table]
		for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
			table, ok := in.imports.table(in.c.tableImportBindingKey(tableIndex))
			if ok && table != nil && tables.add(table) {
				table.retainDescriptorOwnersForFinalizationWithCleanup(in.refStore, in, &cleanup)
			}
		}
		var globals importDedup[*Global]
		for i, imp := range in.c.GlobalImports {
			if imp.Type != ValFuncRef {
				continue
			}
			provided, ok := in.imports.global(in.c.globalImportBindingKey(i))
			if ok && provided.Global != nil && globals.add(provided.Global) {
				provided.Global.retainDescriptorOwnerForFinalizationWithCleanup(in.refStore, in, &cleanup)
			}
		}
		pass := importedContainerFinalizationPass{roots: importedFuncrefProducerRoots(in)}
		var retainedTables, retainedGlobals bool
		cleanup.run(func() { retainedTables = retainProducerRootsInImportedTablesMode(in, true, &pass) })
		cleanup.run(func() { retainedGlobals = retainProducerRootsInImportedGlobalsMode(in, true, &pass) })
		if beforeTransfer != nil {
			cleanup.run(beforeTransfer)
		}
		pass.tables.each(func(table *Table) { cleanup.run(func() { in.transferImportedTableAttachment(table) }) })
		pass.globals.each(func(global *Global) { cleanup.run(func() { in.transferImportedGlobalAttachment(global) }) })
		pass.owners.each(func(owner *Instance) { cleanup.run(func() { in.transferImportedAttachmentsFromOwner(owner) }) })
		cleanup.finish()
		return retainedTables || retainedGlobals
	}
	var retainedTables, retainedGlobals bool
	cleanup.run(func() { retainedTables = retainProducerRootsInImportedTablesMode(in, true, nil) })
	cleanup.run(func() { retainedGlobals = retainProducerRootsInImportedGlobalsMode(in, true, nil) })
	cleanup.finish()
	return retainedTables || retainedGlobals
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
	var cleanup resourceCleanup
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
			cleanup.run(func() { in.transferImportedMemoryAttachment(memory) })
		}
	}

	var globals importDedup[*Global]
	for i := range in.c.GlobalImports {
		provided, ok := in.imports.global(in.c.globalImportBindingKey(i))
		if ok && provided.Global != nil && globals.add(provided.Global) && provided.Global.instanceOwner() == owner {
			cleanup.run(func() { in.transferImportedGlobalAttachment(provided.Global) })
		}
	}

	var tables importDedup[*Table]
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		table, ok := in.imports.table(in.c.tableImportBindingKey(tableIndex))
		if ok && table != nil && tables.add(table) && table.instanceOwner() == owner {
			cleanup.run(func() { in.transferImportedTableAttachment(table) })
		}
	}
	cleanup.finish()
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
		table, ok := in.imports.table(in.c.tableImportBindingKey(tableIndex))
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

func (c *Compiled) hasFuncrefImportContainers() bool {
	if c == nil {
		return false
	}
	scan := func() bool {
		for _, imp := range c.GlobalImports {
			if imp.Type == ValFuncRef {
				return true
			}
		}
		for tableIndex := 0; tableIndex < c.tableImportCount(); tableIndex++ {
			def, _ := c.tableImportAt(tableIndex)
			if def.Type == ValFuncRef {
				return true
			}
		}
		return false
	}
	indexes := c.ensureCompileIndexes()
	if indexes == nil {
		return scan()
	}
	if state := indexes.funcrefImportState.Load(); state != 0 {
		return state == 2
	}
	state := uint32(1)
	if scan() {
		state = 2
	}
	indexes.funcrefImportState.CompareAndSwap(0, state)
	return indexes.funcrefImportState.Load() == 2
}

// mayImportFuncrefStorage is the cheap immutable module-shape check. Callers
// on an import-free hot path can avoid entering the detailed import scan.
func (in *Instance) mayImportFuncrefStorage() bool {
	return in != nil && in.c != nil && (len(in.c.GlobalImports) != 0 || in.c.tableImport != "")
}

func (in *Instance) importsFuncrefStorage() bool {
	if !in.mayImportFuncrefStorage() {
		return false
	}
	return in.c.hasFuncrefImportContainers()
}

type funcrefImportContainers struct {
	once    sync.Once
	globals importDedup[*Global]
	tables  importDedup[*Table]
}

func (in *Instance) funcrefImportContainers() *funcrefImportContainers {
	state := in.ensureImportState()
	containers := state.funcrefImports.Load()
	if containers == nil {
		candidate := new(funcrefImportContainers)
		if state.funcrefImports.CompareAndSwap(nil, candidate) {
			containers = candidate
		} else {
			containers = state.funcrefImports.Load()
		}
	}
	containers.once.Do(func() {
		for i, imp := range in.c.GlobalImports {
			if imp.Type != ValFuncRef {
				continue
			}
			provided, ok := in.imports.global(in.c.globalImportBindingKey(i))
			if ok && provided.Global != nil {
				containers.globals.add(provided.Global)
			}
		}
		for i := 0; i < in.c.tableImportCount(); i++ {
			def, _ := in.c.tableImportAt(i)
			if def.Type != ValFuncRef {
				continue
			}
			table, ok := in.imports.table(in.c.tableImportBindingKey(i))
			if ok && table != nil {
				containers.tables.add(table)
			}
		}
	})
	return containers
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
	imports := in.funcrefImportContainers()
	var cleanup resourceCleanup
	imports.globals.each(func(global *Global) {
		cleanup.run(global.pruneRetainedInstances)
	})
	imports.tables.each(func(table *Table) {
		cleanup.run(table.pruneRetainedInstances)
	})
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
	var localTables importDedup[*Table]
	for table != nil {
		if table.owner != nil && table.owner.elementType == ValFuncRef && !imports.tables.contains(table) && localTables.add(table) {
			cleanup.run(table.pruneRetainedInstances)
		}
		in.lifeMu.Lock()
		table = table.next
		in.lifeMu.Unlock()
	}
	cleanup.finish()
}

func (in *Instance) tableDescriptor(index int) []byte {
	if in == nil || in.c == nil || index < 0 || index >= in.c.tableCount() {
		return nil
	}
	if _, imported := in.c.tableImportAt(index); imported {
		table, ok := in.imports.table(in.c.tableImportBindingKey(index))
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

// Imports returns a sealed caller-owned snapshot of the imports this instance
// was created with. Lookup retrieves imported objects by exact module and name.
func (in *Instance) Imports() *Imports {
	if in.imports == nil {
		return nil
	}
	imports := NewImports()
	for key, value := range in.imports {
		module, name, ok := splitImportBindingMapKey(key)
		if !ok {
			// Preserve legacy hand-built Compiled values used by low-level callers.
			module, name = splitImportKey(key)
		}
		imports.add(module, name, value)
	}
	imports.sealed = true
	return imports
}
