package wago

import (
	"encoding/binary"
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// InstanceExport is a handle to another instance's exported function, used as an
// import value for cross-instance linking. Place it in an Imports map under the
// importing module's "module.name" key; Instantiate then recompiles the importing
// module so calls to that import become a native call into this instance's
// function (see linkModule / emitCrossInstanceCall).
//
// The referenced instance must stay open (not Closed) for as long as any instance
// importing it is in use: the linked code holds its linear-memory and code
// addresses directly.
type InstanceExport struct {
	inst     *Instance
	localIdx int
	params   []ValType
	results  []ValType
}

// ExportedFunc returns a handle to this instance's exported function `name`,
// suitable as a cross-instance import value in another module's Imports. It
// errors if `name` is not an exported (locally-defined) function.
func (in *Instance) ExportedFunc(name string) (*InstanceExport, error) {
	if in == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	li, err := in.c.localIndex(name)
	if err != nil {
		return nil, err
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
	size  int
	arena *coreruntime.Arena // set for host-created tables (NewTable); nil when instance-owned
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
	return &Table{desc: desc, size: size, arena: arena}, nil
}

// Close releases a host-created table's storage. Only call it once every instance
// importing it is closed. A no-op for instance-owned tables.
func (t *Table) Close() error {
	if t == nil || t.arena == nil {
		return nil
	}
	err := t.arena.Close()
	t.arena = nil
	return err
}

// ExportedTable returns this instance's table as a shared *Table another instance
// can import. `name` is advisory (MVP modules have one table).
func (in *Instance) ExportedTable(name string) (*Table, error) {
	if in == nil || in.tableDesc == nil {
		return nil, fmt.Errorf("instance has no table to export")
	}
	if len(in.tableDesc) < 8 {
		return nil, fmt.Errorf("instance table descriptor is invalid")
	}
	return &Table{desc: in.tableDesc, size: int(binary.LittleEndian.Uint32(in.tableDesc))}, nil
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
