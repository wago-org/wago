package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
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
	linMem                 []byte // linear memory, cached once (wago has no memory.grow, so it never moves)
	hosts                  map[string]HostFunc
	imports                Imports // the imports as provided to Instantiate
	hostLog                []byte
	globals                []byte // pointer table handed to JIT code
	globalCells            []*Global
	gc                     *gc.Collector // nil for modules with no Wasm GC descriptors/runtime use
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

// InstantiateOptions configures instance creation.
type InstantiateOptions struct {
	Imports Imports
	GC      GCConfig
}

// Instantiate maps code, wires the module's imports (functions, globals, …) from
// the unified imports namespace, initializes memory/table state, and allocates
// call buffers. Pass nil for a module with no imports.
func Instantiate(c *Compiled, imports Imports) (*Instance, error) {
	return InstantiateWithOptions(c, InstantiateOptions{Imports: imports})
}

// InstantiateWithOptions maps code and applies explicit instance options.
func InstantiateWithOptions(c *Compiled, opts InstantiateOptions) (*Instance, error) {
	imports := opts.Imports
	// Resolve cross-instance function imports, recompiling the module with their
	// bindings when any are present (a no-op for host-only modules).
	c, err := c.linkModule(imports)
	if err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	var collector *gc.Collector
	if gc.HasHeapObjectTypes(c.GCTypeDescs) {
		var err error
		collector, err = gc.NewCollector(opts.GC, c.GCTypeDescs)
		if err != nil {
			return nil, err
		}
	}
	success := false
	defer func() {
		if !success && collector != nil {
			collector.Close()
		}
	}()
	importGlobals, err := c.importedGlobals(imports)
	if err != nil {
		return nil, err
	}
	eng, err := runtime.AcquireEngine()
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
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory with signals-based bounds checks is not supported")
		}
		m, ok := imports.memory(c.memoryImport)
		if !ok {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("missing imported memory %q", c.memoryImport)
		}
		if m.inUse {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory %q is already used by another instance", c.memoryImport)
		}
		m.inUse = true
		jm, memObj = m.jm, m
	} else {
		initialBytes, maxBytes := c.memorySizeBytes()
		if c.boundsMode == BoundsChecksSignalsBased {
			jm, err = newGuardedJobMemory(initialBytes, maxBytes)
		} else {
			jm, err = runtime.AcquireJobMemoryGrowable(initialBytes, maxBytes)
		}
		if err != nil {
			runtime.ReleaseEngine(eng)
			return nil, err
		}
		memObj, ownsMem = &Memory{jm: jm}, true
	}
	// Release the memory only if this instance owns it; an imported *Memory is the
	// host's, so just release the in-use claim.
	closeMem := func() {
		if ownsMem {
			runtime.ReleaseJobMemory(jm)
		} else {
			memObj.inUse = false
		}
	}
	ar, err := runtime.AcquireArena(c.instantiateArenaNeed)
	if err != nil {
		closeMem()
		runtime.ReleaseEngine(eng)
		return nil, err
	}
	base, err := c.acquireCode()
	if err != nil {
		runtime.ReleaseArena(ar)
		closeMem()
		runtime.ReleaseEngine(eng)
		return nil, err
	}
	defer func() {
		if success {
			return
		}
		c.releaseCode()
		runtime.ReleaseArena(ar)
		closeMem()
		runtime.ReleaseEngine(eng)
	}()
	var hostLog []byte
	if len(c.Imports) > 0 {
		hostLog = ar.Alloc(runtime.HostCallLogBytes)
		jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))
	}
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
					if li < len(c.InternalEntry) {
						// Register-ABI internal-entry delta (entry pad word): indirect
						// calls with a compatible signature add this to the code ptr.
						binary.LittleEndian.PutUint32(desc[off+12:], uint32(c.InternalEntry[li]-c.Entry[li]))
					}
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

	argsBytes, err := runtime.SlotBytes(c.maxParamSlots)
	if err != nil {
		return nil, fmt.Errorf("compiled metadata invalid: %w", err)
	}
	resultsBytes, err := runtime.SlotBytes(c.maxResultSlots)
	if err != nil {
		return nil, fmt.Errorf("compiled metadata invalid: %w", err)
	}
	serArgs := ar.Alloc(argsBytes)
	results := ar.Alloc(resultsBytes)
	trap := ar.Alloc(8)

	// Run the start function (() -> ()) now that memory, globals, table, and data
	// are initialized. A trap here aborts instantiation.
	if c.HasStart {
		if c.StartIsImport {
			// Imported start: run the imported function's host binding. Validation
			// guarantees start is () -> (), so it takes no arguments and returns
			// nothing. A cross-instance (non-host) start binding is not yet wired.
			if c.StartImportIdx < 0 || c.StartImportIdx >= len(c.Imports) {
				return nil, fmt.Errorf("start import index %d out of range", c.StartImportIdx)
			}
			key := c.Imports[c.StartImportIdx]
			fn := imports.hostFuncs()[key]
			if fn == nil {
				return nil, fmt.Errorf("start function %q is not a host import", key)
			}
			fn(0)
		} else {
			if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Entry) {
				return nil, fmt.Errorf("start function index %d out of range", c.StartLocalFunc)
			}
			startEntry := base + uintptr(c.Entry[c.StartLocalFunc])
			if err := callNative(c, eng, jm, startEntry, serArgs, trap, results); err != nil {
				return nil, fmt.Errorf("start function trapped: %w", err)
			}
		}
	}

	success = true
	return &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, linMem: jm.CurrentBytes(), hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, globals: globals, globalCells: globalCells, gc: collector,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, c.maxResultSlots),
	}, nil
}

// Close releases the instance's mapped code, engine, and (if instance-owned) its
// memory. An imported memory is left for the host to Close.
func (in *Instance) Close() {
	if in.gc != nil {
		in.gc.Close()
	}
	in.c.releaseCode()
	runtime.ReleaseArena(in.ar)
	if in.ownsMem {
		runtime.ReleaseJobMemory(in.jm)
	} else if in.memory != nil {
		in.memory.inUse = false
	}
	runtime.ReleaseEngine(in.eng)
}

// Memory returns the instance's linear-memory object (instance-owned or the
// host-imported one). Use Memory().Bytes() for the zero-copy byte view.
func (in *Instance) Memory() *Memory { return in.memory }

// Imports returns the imports map this instance was created with, for retrieving
// imported objects (e.g. a *Memory or *Global) by "module.name" key. The map is
// the one passed to Instantiate; do not mutate it.
func (in *Instance) Imports() Imports { return in.imports }
