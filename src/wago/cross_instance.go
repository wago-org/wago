package wago

import (
	"encoding/binary"
	"fmt"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// InstanceExport is a handle to another instance's exported function, used as an
// import value for cross-instance linking. Place it in an Imports map under the
// importing module's "module.name" key; Instantiate then recompiles the importing
// module so calls to that import become a native call into this instance's
// function (see linkModule / emitCrossInstanceCall).
//
// The referenced instance must normally stay open (not Closed) for as long as
// any importing instance can execute it: linked code holds its linear-memory and
// code addresses directly. A same-runtime public funcref token is the exception:
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
	return &InstanceExport{inst: in, localIdx: li, params: sig.Params, results: sig.Results}, nil
}

// Table is a handle to an instance's exported table (its runtime descriptor),
// used as an import value for cross-instance table linking. Both instances then
// share one descriptor, so element writes and call_indirect see the same funcrefs.
// The referenced instance must stay open for as long as any importer is in use.
type Table struct {
	desc  []byte
	arena *coreruntime.Arena // set for host-created tables (NewTable); nil when instance-owned
	next  *Table             // lazy instance-owned export-handle chain

	mu       sync.Mutex
	closed   bool
	retained map[*Instance]struct{}
}

// NewTable creates a host-owned funcref table that modules can import and share
// (e.g. the testsuite's spectest.table). Its entries start empty (an indirect
// call to one traps as uninitialized) until a module populates them via an active
// element segment. maxSize is the table.grow capacity; zero means minSize.
func NewTable(minSize, maxSize uint32) (*Table, error) {
	if maxSize != 0 && maxSize < minSize {
		return nil, fmt.Errorf("wago: table maximum %d < minimum %d", maxSize, minSize)
	}
	if maxSize == 0 {
		maxSize = minSize
	}
	size := int(minSize)
	cap := int(maxSize)
	need := 8 + cap*coreruntime.TableEntryBytes
	arena, err := coreruntime.NewArena(need)
	if err != nil {
		return nil, err
	}
	desc := arena.Alloc(need)
	binary.LittleEndian.PutUint32(desc, uint32(size))
	binary.LittleEndian.PutUint32(desc[4:], uint32(cap))
	return &Table{desc: desc, arena: arena}, nil
}

// Close releases a host-created table's storage. Only call it once every instance
// importing it is closed. A no-op for instance-owned tables.
// Size returns the table's current descriptor length. It reflects table.grow on
// host-created, imported, and re-exported tables.
func (t *Table) Size() int {
	if t == nil || len(t.desc) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(t.desc))
}

func (t *Table) Close() error {
	if t == nil || t.arena == nil {
		return nil
	}
	t.releaseRetainedInstances()
	err := t.arena.Close()
	t.arena = nil
	t.desc = nil
	return err
}

// retainFailedInstance transfers a failed instance's resource lifetime to this
// shared table when one of its local funcrefs remains installed after a start
// trap. Before adding the root, scan refSlot identities and release failed
// instances no longer represented by any entry. This keeps retention bounded by
// the table's finite descriptor capacity even when repeated failed
// instantiations overwrite the same slots.
func (t *Table) retainFailedInstance(in *Instance) bool {
	if t == nil || in == nil || !in.retainResourceRoot() {
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
		if !t.containsLocalFuncref(root) {
			delete(t.retained, root)
			release = append(release, root)
		}
	}
	if !t.containsLocalFuncref(in) {
		t.mu.Unlock()
		in.releaseResourceRoot()
		for _, root := range release {
			root.releaseResourceRoot()
		}
		return false
	}
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

func (t *Table) containsLocalFuncref(in *Instance) bool {
	size := int(binary.LittleEndian.Uint32(t.desc))
	capacity := (len(t.desc) - 8) / coreruntime.TableEntryBytes
	if size > capacity {
		size = capacity
	}
	for slot := 0; slot < size; slot++ {
		off := 8 + slot*coreruntime.TableEntryBytes + coreruntime.TableEntryRefSlotOffset
		if in.ownsLocalFuncrefDescriptor(binary.LittleEndian.Uint64(t.desc[off:])) {
			return true
		}
	}
	return false
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
	if tableIndex == 0 && in.c.tableImport != "" {
		if in.table == nil || len(in.table.desc) < 8 {
			return nil, fmt.Errorf("exported table %q descriptor is invalid", name)
		}
		return in.table, nil
	}
	desc := in.tableDescriptor(tableIndex)
	if len(desc) < 8 {
		return nil, fmt.Errorf("exported table %q index %d descriptor is invalid", name, tableIndex)
	}
	in.lifeMu.Lock()
	for table := in.table; table != nil; table = table.next {
		if len(table.desc) != 0 && &table.desc[0] == &desc[0] {
			in.lifeMu.Unlock()
			return table, nil
		}
	}
	table := &Table{desc: desc, next: in.table}
	in.table = table
	in.lifeMu.Unlock()
	return table, nil
}

// ExportedMemory returns this instance's linear memory as a shared *Memory that
// another instance can import (cross-instance memory linking): the two instances
// then use the same underlying mapping, so stores and memory.grow are mutually
// visible. Only an instance that owns its memory can export it, and — because the
// two share one basedata region — an importer of a shared memory may not declare
// its own globals or table. The referenced instance must stay open for as long as
// any importer is in use. `name` is advisory (MVP modules have one memory).
func (in *Instance) ExportedMemory(name string) (*Memory, error) {
	if in == nil || in.memory == nil {
		return nil, fmt.Errorf("instance has no memory to export")
	}
	if !in.ownsMem {
		return nil, fmt.Errorf("cannot re-export an imported memory")
	}
	in.memory.shared = true
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
	idx, ok := in.c.GlobalExports[name]
	if !ok {
		return nil, fmt.Errorf("no exported global %q", name)
	}
	if idx < 0 || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
		return nil, fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	return in.globalCells[idx], nil
}
