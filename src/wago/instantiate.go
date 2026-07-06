package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot"
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
	hosts                  map[string]HostFunc
	imports                Imports // the imports as provided to Instantiate
	hostLog                []byte
	syncMode               bool             // true when host imports use the synchronous re-entry protocol
	ctrl                   []byte           // sync host-call control frame (nil in async mode)
	syncHosts              []SyncHostFunc   // per import-func-index host, sync mode only
	hostCall               runtime.HostCall // per-instance sync host dispatcher, allocated once
	globals                []byte           // pointer table handed to JIT code
	globalCells            []*Global
	tableDesc              []byte        // owned table descriptor (nil when imported), for cross-instance export
	thunkMem               []byte        // executable mapping for host-func-in-table log thunks (nil if none)
	gc                     *gc.Collector // nil for modules with no Wasm GC descriptors/runtime use
	serArgs, results, trap []byte
	resultVals             []uint64    // reusable Invoke result buffer (valid until the next call)
	ic                     invokeCache // single-entry export resolution cache

	// rt is set when the instance is created through a Runtime (rt.Instantiate /
	// Spawn), so Instance.Call can fire the runtime's invoke hooks. It is nil for
	// the low-level package-level Instantiate, which stays hook-free.
	rt *Runtime
}

// invokeCache memoizes per-export work so a hot Invoke loop on one export skips
// the exports map probe and the fat ValType width comparisons on every call.
type invokeCache struct {
	export      string
	valid       bool
	li          int
	paramSlots  int
	resultSlots int
	resultWide  []bool // one entry per returned uint64 slot; false means read low 32 bits
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
	if err := c.validateCached(); err != nil {
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
		m, ok := imports.memory(c.memoryImport)
		if !ok {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("missing imported memory %q", c.memoryImport)
		}
		// A signals-based module elides inline bounds checks and relies on the
		// guard-page fault, so the imported memory must be guard-page backed. Host
		// NewMemory and guard-page instance owners provide one only in a
		// wago_guardpage build; reject a plain mapping (e.g. an explicit-bounds
		// owner's memory, or a deserialized signals-based module in a default binary).
		if c.boundsMode == BoundsChecksSignalsBased && !m.guarded {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory %q is not guard-page backed; signals-based bounds checks require a guard-page memory (build with -tags wago_guardpage)", c.memoryImport)
		}
		if m.shared {
			// Cross-instance shared memory: the importer runs on the owner's jm, so
			// it also shares the owner's basedata. That is only safe when the importer
			// declares no globals and no OWN table, which would overwrite the owner's
			// basedata slots. An imported table is fine — it repoints offTablePtr to a
			// shared descriptor (typically the same owner's), not a new one.
			hasLocalTable := c.HasTable && c.tableImport == ""
			if len(c.Globals) > 0 || hasLocalTable {
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("a module importing a shared memory may not declare its own globals or table")
			}
			jm, memObj = m.jm, m
		} else {
			if m.inUse {
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("imported memory %q is already used by another instance", c.memoryImport)
			}
			m.inUse = true
			jm, memObj = m.jm, m
		}
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
		memObj, ownsMem = &Memory{jm: jm, guarded: c.boundsMode == BoundsChecksSignalsBased}, true
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
	var thunkMem []byte // host-func-in-table log thunks; unmapped on failure/close
	defer func() {
		if success {
			return
		}
		if thunkMem != nil {
			runtime.Unmap(thunkMem)
		}
		c.releaseCode()
		runtime.ReleaseArena(ar)
		closeMem()
		runtime.ReleaseEngine(eng)
	}()
	var hostLog, ctrl []byte
	var syncHosts []SyncHostFunc
	if c.syncHostImports {
		// Synchronous host-call path: install the control frame (not the async
		// log) as the import ctx and bind every host import to a SyncHostFunc.
		ctrl = ar.AllocNoZero(runtime.HostCtrlFrameBytes)
		jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
		syncHosts, err = c.buildSyncHosts(imports)
		if err != nil {
			return nil, fmt.Errorf("instantiate: %w", err)
		}
	} else if len(c.Imports) > 0 {
		for _, key := range c.Imports {
			if _, cross := imports[key].(*InstanceExport); cross {
				continue
			}
			fn, ok := imports[key].(HostFunc)
			if !ok || fn == nil {
				return nil, fmt.Errorf("import %q: legacy async host calls require wago.HostFunc", key)
			}
		}
		// The log's count header is reset at the start of every Invoke and its
		// body is written by native code before the host reads it, so the ~64 KiB
		// buffer needs no instantiate-time zero-fill.
		hostLog = ar.AllocNoZero(runtime.HostCallLogBytes)
		jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))
	}
	jm.SetStackFence(eng.StackLimit()) // trap runaway recursion instead of faulting

	var globals []byte
	globalCells := make([]*Global, len(c.Globals))
	if len(c.Globals) > 0 {
		globals = ar.Alloc(8 * len(c.Globals))
		// One heap allocation backs every module-local global cell (a *Global into
		// this slab) instead of one allocation per global; imported globals keep
		// their own cached *Global.
		localCells := make([]Global, len(c.Globals))
		// Wasm global indexes are stored in order in a pointer table: imported
		// global objects first, followed by module-local cells initialized from
		// literal bits or by copying an earlier imported immutable global's value.
		for i, g := range c.Globals {
			var cell *Global
			if i < len(importGlobals) {
				imp := importGlobals[i]
				if imp.global == nil {
					imp.global = newGlobalInCell(imp.initialType, imp.initialBits, imp.initialV128, imp.mutable, ar.Alloc(globalCellSize(imp.initialType)), nil)
				}
				cell = imp.global
			} else {
				bits, vec := g.Bits, g.V128
				if g.HasInitGlobal {
					if g.InitGlobal < 0 || g.InitGlobal >= i || globalCells[g.InitGlobal] == nil {
						return nil, fmt.Errorf("global %d initializer references unavailable global %d", i, g.InitGlobal)
					}
					bits = readGlobalObject(globalCells[g.InitGlobal], c.Globals[g.InitGlobal].Type)
					vec = readGlobalObjectV128(globalCells[g.InitGlobal])
				}
				cell = &localCells[i]
				cell.Type, cell.Mutable, cell.cell = g.Type, g.Mutable, ar.Alloc(globalCellSize(g.Type))
				writeGlobalObject(cell, g.Type, bits)
				if g.Type == ValV128 {
					writeGlobalObjectV128(cell, vec)
				}
			}
			globalCells[i] = cell
			binary.LittleEndian.PutUint64(globals[i*8:], uint64(uintptr(unsafe.Pointer(&cell.cell[0]))))
		}
		jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
	}

	// Table descriptor: [len u32][pad][entry...], 32-byte entries
	// {codePtr u64, sigID u32, pad u32, homeLinMem u64, pad u64}. homeLinMem is the
	// linear-memory base of the instance the funcref belongs to, so call_indirect
	// runs each entry in its home context (cross-instance funcrefs swap context;
	// same-instance entries take a fast path). Allocate the descriptor even for a
	// zero-length table so call_indirect reads len=0 and traps out-of-bounds.
	var tableDesc []byte
	if c.HasTable {
		var desc []byte
		var size int
		if c.tableImport != "" {
			// Shared cross-instance table: run on the exporting instance's descriptor.
			t, ok := imports.table(c.tableImport)
			if !ok {
				return nil, fmt.Errorf("missing imported table %q", c.tableImport)
			}
			desc, size = t.desc, t.size
		} else {
			size = c.TableSize
			desc = ar.Alloc(8 + size*runtime.TableEntryBytes)
			binary.LittleEndian.PutUint32(desc, uint32(size))
			tableDesc = desc // owned; exportable to other instances
		}
		selfLinMem := uint64(jm.LinMemBase())
		// Host functions placed in the table (used as funcrefs) get a per-instance
		// thunk as their code pointer: legacy async HostFunc imports log to the host
		// replay buffer, while sync-mode imports marshal through the control frame.
		thunkAddr, tmem, terr := buildHostFuncThunks(c, imports)
		if terr != nil {
			return nil, terr
		}
		thunkMem = tmem
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
				off := 8 + slot*runtime.TableEntryBytes
				if li := int(fidx) - c.NumImports; li >= 0 && li < len(c.Entry) {
					// Local function: runs in this instance's context.
					binary.LittleEndian.PutUint64(desc[off:], uint64(base)+uint64(c.Entry[li])) // offset-0 entry
					binary.LittleEndian.PutUint64(desc[off+16:], selfLinMem)                    // home = this instance
				} else if int(fidx) < c.NumImports {
					// Imported function: a cross-instance funcref runs in its home
					// instance's context (call_indirect swaps to it); a host-function
					// funcref points at this instance's log thunk. Anything else stays
					// null (an indirect call traps).
					if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.localIdx < len(ex.inst.c.Entry) {
						binary.LittleEndian.PutUint64(desc[off:], uint64(ex.inst.base)+uint64(ex.inst.c.Entry[ex.localIdx]))
						binary.LittleEndian.PutUint64(desc[off+16:], uint64(ex.inst.jm.LinMemBase()))
					} else if addr, ok := thunkAddr[fidx]; ok {
						binary.LittleEndian.PutUint64(desc[off:], addr)          // host log thunk
						binary.LittleEndian.PutUint64(desc[off+16:], selfLinMem) // home = this instance
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

	in := &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, syncMode: c.syncHostImports, ctrl: ctrl, syncHosts: syncHosts, globals: globals, globalCells: globalCells, tableDesc: tableDesc, thunkMem: thunkMem, gc: collector,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, c.maxResultSlots),
	}
	if in.syncMode {
		in.hostCall = in.newHostDispatch()
	}

	// Run the start function (() -> ()) now that memory, globals, table, and data
	// are initialized. A trap here aborts instantiation.
	if c.HasStart {
		if c.StartIsImport {
			// Imported start: run the imported function through the same normalized
			// binding machinery used by ordinary host imports. Validation guarantees
			// start is () -> (). Cross-instance imported starts remain unsupported.
			if c.StartImportIdx < 0 || c.StartImportIdx >= len(c.Imports) {
				return nil, fmt.Errorf("start import index %d out of range", c.StartImportIdx)
			}
			key := c.Imports[c.StartImportIdx]
			if ex, ok := imports[key].(*InstanceExport); ok && ex != nil {
				return nil, fmt.Errorf("start function %q is a cross-instance import; cross-instance imported starts are unsupported", key)
			}
			fn, err := bindHostImport(imports[key], FuncSig{})
			if err != nil {
				return nil, fmt.Errorf("start function %q: %w", key, err)
			}
			fn(instanceHostModule{in: in}, nil, nil)
		} else {
			if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Entry) {
				return nil, fmt.Errorf("start function index %d out of range", c.StartLocalFunc)
			}
			startEntry := base + uintptr(c.Entry[c.StartLocalFunc])
			if in.syncMode {
				if err := in.callNativeSync(startEntry); err != nil {
					return nil, fmt.Errorf("start function trapped: %w", err)
				}
			} else if err := callNative(c, eng, jm, startEntry, serArgs, trap, results); err != nil {
				return nil, fmt.Errorf("start function trapped: %w", err)
			}
		}
	}

	success = true
	return in, nil
}

// buildHostFuncThunks generates a per-instance executable mapping of thunks for
// host functions placed in the module's table (used as funcrefs), returning each
// such import's thunk entry address and the mapping (nil when none). A
// call_indirect through a legacy async HostFunc thunk logs the host call for the
// normal post-invoke replay; a sync-mode thunk uses the synchronous control frame
// and returns the host results through the wrapper result slots.
func buildHostFuncThunks(c *Compiled, imports Imports) (map[uint32]uint64, []byte, error) {
	var blob []byte
	offs := map[uint32]int{}
	for _, el := range c.Elems {
		for _, fidx := range el.Funcs {
			if int(fidx) >= c.NumImports {
				continue
			}
			key := c.Imports[fidx]
			if _, isCross := imports[key].(*InstanceExport); isCross {
				continue // cross-instance funcref, not a host function
			}
			if _, done := offs[fidx]; done {
				continue
			}
			if c.syncHostImports {
				if int(fidx) >= len(c.importFuncSigs) {
					return nil, nil, fmt.Errorf("import %q appears in a table but its signature is missing", key)
				}
				sig := c.importFuncSigs[fidx]
				paramSlots, err := valTypesSlots(sig.Params)
				if err != nil {
					return nil, nil, fmt.Errorf("import %q table thunk params: %w", key, err)
				}
				resultSlots, err := valTypesSlots(sig.Results)
				if err != nil {
					return nil, nil, fmt.Errorf("import %q table thunk results: %w", key, err)
				}
				if paramSlots > runtime.MaxHostArity || resultSlots > runtime.MaxHostArity {
					return nil, nil, fmt.Errorf("import %q appears in a table and uses %d param slot(s), %d result slot(s); synchronous table host funcrefs support at most %d slots in each direction", key, paramSlots, resultSlots, runtime.MaxHostArity)
				}
				offs[fidx] = len(blob)
				blob = append(blob, railshot.HostIndirectSyncThunk(fidx, paramSlots, resultSlots)...)
				continue
			}
			if _, isHost := imports[key].(HostFunc); !isHost {
				if imports[key] != nil {
					return nil, nil, fmt.Errorf("import %q appears in a table but is %T; table host funcrefs in async mode support only legacy wago.HostFunc bindings", key, imports[key])
				}
				continue
			}
			offs[fidx] = len(blob)
			blob = append(blob, railshot.HostIndirectThunk(fidx)...)
		}
	}
	if len(blob) == 0 {
		return nil, nil, nil
	}
	mem, base, err := runtime.MapCode(blob)
	if err != nil {
		return nil, nil, fmt.Errorf("host-func table thunk: %w", err)
	}
	addr := make(map[uint32]uint64, len(offs))
	for fidx, o := range offs {
		addr[fidx] = uint64(base) + uint64(o)
	}
	return addr, mem, nil
}

// Close releases the instance's mapped code, engine, and (if instance-owned) its
// memory. An imported memory is left for the host to Close.
func (in *Instance) Close() {
	if in.gc != nil {
		in.gc.Close()
	}
	if in.thunkMem != nil {
		runtime.Unmap(in.thunkMem)
		in.thunkMem = nil
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
