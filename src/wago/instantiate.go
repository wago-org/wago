package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
)

// Instance is ready for repeated Invoke calls.
type Instance struct {
	c                      *Compiled
	eng                    *runtime.Engine
	jm                     *runtime.JobMemory
	memory                 *Memory // the memory object (owned or host-imported)
	ownsMem                bool    // false when memory is host-imported (don't close it)
	ar                     *runtime.Arena
	base                   uintptr
	mem                    []byte // mapped code (for Unmap)
	linMem                 []byte // linear memory, cached once (wago has no memory.grow, so it never moves)
	hosts                  map[string]HostFunc
	imports                Imports // the imports as provided to Instantiate
	hostLog                []byte
	globals                []byte // pointer table handed to JIT code
	globalCells            []*Global
	serArgs, results, trap []byte
	resultVals             []uint64    // reusable Invoke result buffer (valid until the next call)
	ic                     invokeCache // single-entry export resolution cache
}

// invokeCache memoizes per-export work so a hot Invoke loop on one export skips
// the exports map probe and the fat ValType width comparisons on every call.
type invokeCache struct {
	export     string
	valid      bool
	li         int
	resultWide []bool // true when the result occupies 8 bytes (i64/f64)
}

// Instantiate maps code, wires the module's imports (functions, globals, …) from
// the unified imports namespace, initializes memory/table state, and allocates
// call buffers. Pass nil for a module with no imports.
func Instantiate(c *Compiled, imports Imports) (*Instance, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	importGlobals, err := c.importedGlobals(imports)
	if err != nil {
		return nil, err
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		return nil, err
	}
	// Memory: a host-imported *Memory if the module imports one, otherwise an
	// instance-owned mapping (guard-page-backed for signals-based modules, so the
	// fault handler catches OOB accesses through the normal Invoke path).
	var (
		jm      *runtime.JobMemory
		memObj  *Memory
		ownsMem bool
	)
	if c.memoryImport != "" {
		if c.boundsMode == BoundsChecksSignalsBased {
			eng.Close()
			return nil, fmt.Errorf("imported memory with signals-based bounds checks is not supported")
		}
		m, ok := imports.memory(c.memoryImport)
		if !ok {
			eng.Close()
			return nil, fmt.Errorf("missing imported memory %q", c.memoryImport)
		}
		if m.inUse {
			eng.Close()
			return nil, fmt.Errorf("imported memory %q is already used by another instance", c.memoryImport)
		}
		m.inUse = true
		jm, memObj = m.jm, m
	} else {
		initialBytes, maxBytes := c.memorySizeBytes()
		if c.boundsMode == BoundsChecksSignalsBased {
			jm, err = newGuardedJobMemory(initialBytes)
		} else {
			jm, err = runtime.NewJobMemoryGrowable(initialBytes, maxBytes)
		}
		if err != nil {
			eng.Close()
			return nil, err
		}
		memObj, ownsMem = &Memory{jm: jm}, true
	}
	// Release the memory only if this instance owns it; an imported *Memory is the
	// host's, so just release the in-use claim.
	closeMem := func() {
		if ownsMem {
			jm.Close()
		} else {
			memObj.inUse = false
		}
	}
	ar, err := runtime.NewArena(runtime.InstantiateArenaSize)
	if err != nil {
		closeMem()
		eng.Close()
		return nil, err
	}
	mem, base, err := runtime.MapCode(c.Code)
	if err != nil {
		ar.Close()
		closeMem()
		eng.Close()
		return nil, err
	}
	success := false
	defer func() {
		if success {
			return
		}
		runtime.Unmap(mem)
		ar.Close()
		closeMem()
		eng.Close()
	}()
	const maxEntries = (1 << 16) / 8
	hostLog := ar.Alloc(8 + maxEntries*8)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))
	jm.SetStackFence(eng.StackLimit()) // trap runaway recursion instead of faulting

	var globals []byte
	globalCells := make([]*Global, len(c.Globals))
	if len(c.Globals) > 0 {
		globals = ar.Alloc(8 * len(c.Globals))
		// Wasm global indexes are stored in order in a pointer table: imported
		// global objects first, followed by module-local cells initialized from
		// literal bits or by copying an earlier imported immutable global's value.
		for i, g := range c.Globals {
			var cell *Global
			if i < len(importGlobals) {
				imp := importGlobals[i]
				if imp.global == nil {
					imp.global = newGlobalInCell(imp.initialType, imp.initialBits, imp.mutable, ar.Alloc(8), nil)
				}
				cell = imp.global
			} else {
				bits := g.Bits
				if g.HasInitGlobal {
					if g.InitGlobal < 0 || g.InitGlobal >= i || globalCells[g.InitGlobal] == nil {
						return nil, fmt.Errorf("global %d initializer references unavailable global %d", i, g.InitGlobal)
					}
					bits = readGlobalObject(globalCells[g.InitGlobal], c.Globals[g.InitGlobal].Type)
				}
				cell = newGlobalInCell(g.Type, bits, g.Mutable, ar.Alloc(8), nil)
			}
			globalCells[i] = cell
			binary.LittleEndian.PutUint64(globals[i*8:], uint64(uintptr(unsafe.Pointer(&cell.cell[0]))))
		}
		jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
	}

	// Table descriptor: [len u32][pad][entry...], entry {codePtr u64, sigID u32, pad u32}.
	// Allocate it even for zero-length tables so call_indirect can read len=0 and
	// trap as out-of-bounds instead of dereferencing an absent descriptor.
	if c.HasTable {
		size := c.TableSize
		desc := ar.Alloc(8 + size*16)
		binary.LittleEndian.PutUint32(desc, uint32(size))
		for seg, el := range c.Elems {
			elemBase := el.Offset.Base
			if el.Offset.HasGlobal {
				if el.Offset.Global < 0 || el.Offset.Global >= len(c.Globals) || el.Offset.Global >= len(globalCells) || globalCells[el.Offset.Global] == nil {
					return nil, fmt.Errorf("element offset global %d out of range", el.Offset.Global)
				}
				elemBase = uint32(readGlobalObject(globalCells[el.Offset.Global], c.Globals[el.Offset.Global].Type))
			}
			end := uint64(elemBase) + uint64(len(el.Funcs))
			if end > uint64(size) {
				return nil, fmt.Errorf("active element segment %d out of bounds: offset %d + length %d > table size %d", seg, elemBase, len(el.Funcs), size)
			}
			for k, fidx := range el.Funcs {
				slot := int(elemBase) + k
				off := 8 + slot*16
				if li := int(fidx) - c.NumImports; li >= 0 && li < len(c.Entry) {
					binary.LittleEndian.PutUint64(desc[off:], uint64(base)+uint64(c.Entry[li]))
				}
				if int(fidx) < len(c.FuncTypeID) {
					binary.LittleEndian.PutUint32(desc[off+8:], c.FuncTypeID[fidx])
				}
			}
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&desc[0])))
	}

	if len(c.Data) > 0 {
		lin := jm.CurrentBytes() // active data must fit the initial size, not the reservation
		for seg, d := range c.Data {
			off := d.Offset.Base
			if d.Offset.HasGlobal {
				if d.Offset.Global < 0 || d.Offset.Global >= len(c.Globals) || d.Offset.Global >= len(globalCells) || globalCells[d.Offset.Global] == nil {
					return nil, fmt.Errorf("data offset global %d out of range", d.Offset.Global)
				}
				off = uint32(readGlobalObject(globalCells[d.Offset.Global], c.Globals[d.Offset.Global].Type))
			}
			end := uint64(off) + uint64(len(d.Bytes))
			if end > uint64(len(lin)) {
				return nil, fmt.Errorf("active data segment %d out of bounds: offset %d + length %d > memory size %d", seg, off, len(d.Bytes), len(lin))
			}
			copy(lin[off:end], d.Bytes)
		}
	}

	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return nil, fmt.Errorf("compiled metadata invalid: %w", err)
	}
	argsBytes, err := runtime.SlotBytes(maxParams)
	if err != nil {
		return nil, fmt.Errorf("compiled metadata invalid: %w", err)
	}
	resultsBytes, err := runtime.SlotBytes(maxResults)
	if err != nil {
		return nil, fmt.Errorf("compiled metadata invalid: %w", err)
	}
	serArgs := ar.Alloc(argsBytes)
	results := ar.Alloc(resultsBytes)
	trap := ar.Alloc(8)

	// Run the start function (() -> ()) now that memory, globals, table, and data
	// are initialized. A trap here aborts instantiation. Host imports the start
	// calls are deferred to the log, so reset it before and replay it after,
	// exactly as Invoke does.
	if c.HasStart {
		if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Entry) {
			return nil, fmt.Errorf("start function index %d out of range", c.StartLocalFunc)
		}
		binary.LittleEndian.PutUint32(hostLog, 0)
		startEntry := base + uintptr(c.Entry[c.StartLocalFunc])
		if err := eng.Call(startEntry, serArgs, jm.LinearMemory(), trap, results); err != nil {
			return nil, fmt.Errorf("start function trapped: %w", err)
		}
		replayHostCalls(hostLog, c.Imports, imports.hostFuncs())
	}

	success = true
	return &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, mem: mem, linMem: jm.CurrentBytes(), hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, globals: globals, globalCells: globalCells,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, maxResults),
	}, nil
}

// Close releases the instance's mapped code, engine, and (if instance-owned) its
// memory. An imported memory is left for the host to Close.
func (in *Instance) Close() {
	runtime.Unmap(in.mem)
	in.ar.Close()
	if in.ownsMem {
		in.jm.Close()
	} else if in.memory != nil {
		in.memory.inUse = false
	}
	in.eng.Close()
}

// Memory returns the instance's linear-memory object (instance-owned or the
// host-imported one). Use Memory().Bytes() for the zero-copy byte view.
func (in *Instance) Memory() *Memory { return in.memory }

// Imports returns the imports map this instance was created with, for retrieving
// imported objects (e.g. a *Memory or *Global) by "module.name" key. The map is
// the one passed to Instantiate; do not mutate it.
func (in *Instance) Imports() Imports { return in.imports }
