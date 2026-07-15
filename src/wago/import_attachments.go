package wago

import (
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"
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
	set importDedup[*HostFuncRef]
}

func (a *hostFuncRefAttachments) attach(owner *HostFuncRef, store *referenceStore, sig FuncSig) error {
	if owner == nil {
		return fmt.Errorf("host funcref owner is nil")
	}
	if a.set.contains(owner) {
		return owner.validateImport(store, sig)
	}
	if err := owner.attachImporter(store, sig); err != nil {
		return err
	}
	a.set.push(owner)
	return nil
}

func (a *hostFuncRefAttachments) detachAll() {
	a.set.each((*HostFuncRef).detachImporter)
	a.set.reset()
}

func detachImportedHostFuncRefs(in *Instance) {
	if in == nil || in.c == nil {
		return
	}
	var seen importDedup[*HostFuncRef]
	for _, key := range in.c.Imports {
		owner, ok := in.imports[key].(*HostFuncRef)
		if !ok || owner == nil {
			continue
		}
		if seen.add(owner) {
			owner.detachImporter()
		}
	}
}

type globalImportAttachments struct {
	set importDedup[*Global]
}

func (a *globalImportAttachments) attach(global *Global, store *referenceStore) error {
	if global == nil {
		return fmt.Errorf("global is nil")
	}
	validate := global.validateNumericImport
	attach := global.attachNumericImporter
	if isReferenceValType(global.Type) {
		validate = func() error { return global.validateReferenceImport(store) }
		attach = func() error { return global.attachReferenceImporter(store) }
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

type transferredImportAttachmentState struct {
	mu      sync.Mutex
	tables  map[*Table]struct{}
	globals map[*Global]struct{}
}

var transferredImportAttachments sync.Map // map[*Instance]*transferredImportAttachmentState

func transferredImportState(in *Instance) *transferredImportAttachmentState {
	state := &transferredImportAttachmentState{}
	actual, _ := transferredImportAttachments.LoadOrStore(in, state)
	return actual.(*transferredImportAttachmentState)
}

func (in *Instance) transferImportedGlobalAttachment(global *Global) {
	if in == nil || global == nil {
		return
	}
	state := transferredImportState(in)
	state.mu.Lock()
	if state.globals == nil {
		state.globals = make(map[*Global]struct{})
	}
	_, exists := state.globals[global]
	if !exists {
		state.globals[global] = struct{}{}
	}
	state.mu.Unlock()
	if !exists {
		global.detachReferenceImporter()
	}
}

func (in *Instance) ownsTransferredGlobalAttachment(global *Global) bool {
	value, ok := transferredImportAttachments.Load(in)
	if !ok {
		return false
	}
	state := value.(*transferredImportAttachmentState)
	state.mu.Lock()
	_, ok = state.globals[global]
	state.mu.Unlock()
	return ok
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
		if provided.Global.retainProducerInstance(in) {
			in.transferImportedGlobalAttachment(provided.Global)
			retained = true
		}
	}
	return retained
}

type tableImportAttachments struct {
	set importDedup[*Table]
}

func (a *tableImportAttachments) attach(table *Table, elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, addr64 bool) error {
	if err := table.validateImport(elementType, exact, types, store, addr64); err != nil {
		return err
	}
	if a.set.contains(table) {
		return nil
	}
	if err := table.attachImporter(elementType, exact, types, store, addr64); err != nil {
		return err
	}
	a.set.push(table)
	return nil
}

func (a *tableImportAttachments) detachAll() {
	a.set.each((*Table).detachImporter)
	a.set.reset()
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

func (in *Instance) transferImportedTableAttachment(table *Table) {
	if in == nil || table == nil {
		return
	}
	state := transferredImportState(in)
	state.mu.Lock()
	if state.tables == nil {
		state.tables = make(map[*Table]struct{})
	}
	_, exists := state.tables[table]
	if !exists {
		state.tables[table] = struct{}{}
	}
	state.mu.Unlock()
	if !exists {
		table.detachImporter()
	}
}

func (in *Instance) ownsTransferredTableAttachment(table *Table) bool {
	value, ok := transferredImportAttachments.Load(in)
	if !ok {
		return false
	}
	state := value.(*transferredImportAttachmentState)
	state.mu.Lock()
	_, ok = state.tables[table]
	state.mu.Unlock()
	return ok
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
			table.detachImporter()
		}
	}
}

func retainProducerRootsInImportedTables(in *Instance) bool {
	if in == nil || in.c == nil {
		return false
	}
	retained := false
	for tableIndex := 0; tableIndex < in.c.tableImportCount(); tableIndex++ {
		def, _ := in.c.tableImportAt(tableIndex)
		table, ok := in.imports.table(def.Key)
		if ok && table.retainProducerInstance(in) {
			in.transferImportedTableAttachment(table)
			retained = true
		}
	}
	return retained
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
	in.lifeMu.Lock()
	for table := in.table; table != nil; table = table.next {
		if table.owner != nil && table.owner.elementType == ValFuncRef && tables.add(table) {
			table.pruneRetainedInstances()
		}
	}
	in.lifeMu.Unlock()
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
	def := in.c.tableDef(index)
	capacity := def.Max
	if capacity == 0 {
		capacity = def.Size
	}
	return unsafe.Slice((*byte)(offHeapPtr(descPtr)), 8+capacity*in.c.tableEntryBytes(index))
}

// Imports returns the imports map this instance was created with, for retrieving
// imported objects (e.g. a *Memory or *Global) by "module.name" key. The map is
// the one passed to Instantiate; do not mutate it.
func (in *Instance) Imports() Imports { return in.imports }
