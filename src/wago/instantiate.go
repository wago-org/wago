package wago

import (
	"encoding/binary"
	"errors"
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
	hosts                  map[string]HostFunc
	imports                Imports // the imports as provided to Instantiate
	hostLog                []byte
	syncMode               bool             // true when host imports use the synchronous re-entry protocol
	ctrl                   []byte           // sync host-call control frame (nil in async mode)
	syncHosts              []HostFunc       // per import-func-index host, sync mode only
	hostCall               runtime.HostCall // per-instance sync host dispatcher, allocated once
	globals                []byte           // pointer table handed to JIT code
	globalCells            []*Global
	tableDesc              []byte        // table descriptor view (owned locally or imported), for cross-instance export
	funcRefDescs           []byte        // canonical funcref descriptor handles for this instance's function index space
	passiveDataDesc        []byte        // per-instance passive-data descriptors; data.drop mutates lengths
	thunkMem               []byte        // executable mapping for host-func-in-table log thunks (nil if none)
	gc                     *gc.Collector // nil for modules with no Wasm GC descriptors/runtime use
	serArgs, results, trap []byte
	resultVals             []uint64       // reusable Invoke result buffer (valid until the next call)
	ic                     [4]invokeCache // tiny fixed export resolution cache
	icNext                 uint8          // round-robin replacement cursor
	closed                 bool           // guards Close against double-release (user defer + pool)

	// rt is set when the instance is created through a Runtime (rt.Instantiate /
	// Spawn), so Instance.Call can fire the runtime's invoke hooks. It is nil for
	// the low-level package-level Instantiate, which stays hook-free.
	rt *Runtime
}

// invokeCache memoizes per-export work so hot Invoke loops skip the exports map
// probe and the fat ValType width comparisons on every call. Instance keeps a
// few fixed slots because real AS loops commonly interleave the business export
// with __collect, __pin, or paired request/response exports.
type invokeCache struct {
	export      string
	valid       bool
	li          int
	paramSlots  int
	resultSlots int
	resultWide  []bool // one entry per returned uint64 slot; false means read low 32 bits
}

// InstantiateOptions configures instance creation from a *Compiled. When
// instantiating from a *Snapshot both fields are ignored: the snapshot carries
// the imports and GC config it was created with.
type InstantiateOptions struct {
	Imports Imports
	GC      GCConfig

	// restore, when set, seeds the new instance from a captured Snapshot instead
	// of the module's declared initial state: linear memory and module-local
	// globals are loaded from the snapshot, and active data segments plus the
	// start function are skipped. Table modules are rejected by Capture until
	// table state is snapshotted too. Set only via the *Snapshot instantiation path;
	// unexported so it stays an internal instantiation mode.
	restore *Snapshot
}

// Instantiable is the set of sources Instantiate accepts: a compiled module or a
// captured snapshot. The interface is sealed — only *Compiled and *Snapshot
// implement it.
type Instantiable interface {
	instantiable()
}

func (*Compiled) instantiable() {}
func (*Snapshot) instantiable() {}

// Instantiate creates a live instance from either a *Compiled (wiring the
// module's imports from opts and running its start function) or a *Snapshot
// (loading captured memory/globals; opts is ignored — the snapshot supplies its
// own imports and GC config, and the start function is not re-run).
//
// It accepts InstantiateOptions, Imports, nil, or no second argument. The Imports
// form keeps older callers source-compatible while the options struct remains the
// extensible form for newer code.
func Instantiate(source Instantiable, opts ...any) (*Instance, error) {
	instOpts, err := instantiateArgs(opts)
	if err != nil {
		return nil, err
	}
	switch s := source.(type) {
	case *Compiled:
		return instantiateCore(s, instOpts)
	case *Snapshot:
		if s == nil || s.c == nil {
			return nil, errors.New("wago: snapshot has no bound module (load it with LoadSnapshot)")
		}
		return instantiateCore(s.c, InstantiateOptions{Imports: s.imports, GC: s.gc, restore: s})
	case nil:
		return nil, errors.New("wago: Instantiate: nil source")
	default:
		return nil, fmt.Errorf("wago: Instantiate: unsupported source %T", source)
	}
}

func instantiateArgs(args []any) (InstantiateOptions, error) {
	switch len(args) {
	case 0:
		return InstantiateOptions{}, nil
	case 1:
		switch v := args[0].(type) {
		case nil:
			return InstantiateOptions{}, nil
		case InstantiateOptions:
			return v, nil
		case *InstantiateOptions:
			if v == nil {
				return InstantiateOptions{}, nil
			}
			return *v, nil
		case Imports:
			return InstantiateOptions{Imports: v}, nil
		default:
			return InstantiateOptions{}, fmt.Errorf("wago: Instantiate options must be InstantiateOptions, Imports, or nil, got %T", args[0])
		}
	default:
		return InstantiateOptions{}, fmt.Errorf("wago: Instantiate expects at most one options argument, got %d", len(args))
	}
}

// instantiateCore maps code and applies explicit instance options. It is the
// shared engine behind Instantiate for both the compiled and snapshot paths.
func instantiateCore(c *Compiled, opts InstantiateOptions) (*Instance, error) {
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
			// declares no globals, no OWN table, and no passive data descriptor array,
			// any of which would overwrite the owner's basedata slots. An imported
			// table is fine — it repoints offTablePtr to a shared descriptor (typically
			// the same owner's), not a new one.
			hasLocalTable := c.HasTable && c.tableImport == ""
			if len(c.Globals) > 0 || hasLocalTable || len(c.PassiveData) > 0 {
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("a module importing a shared memory may not declare its own globals, table, or passive data")
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
		// Restoring from a snapshot: size the fresh mapping to the snapshot's
		// (possibly grown) linear-memory size so the saved bytes fit and memory.size
		// reports the captured value, not the module's declared minimum.
		if opts.restore != nil {
			if rb := int(opts.restore.memPages) * 65536; rb > initialBytes {
				initialBytes = rb
				if initialBytes > maxBytes {
					maxBytes = initialBytes
				}
			}
		}
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
	var syncHosts []HostFunc
	if c.syncHostImports {
		// Synchronous host-call path: install the control frame (not the async
		// log) as the import ctx and bind every host import to a HostFunc.
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
		// Snapshot restore: replace each module-local global's freshly-initialized
		// value with the captured one. Imported globals (the leading cells) keep the
		// value of whatever the caller re-imported this time — their state lives in
		// the host, not in the snapshot.
		if opts.restore != nil {
			for i := len(importGlobals); i < len(globalCells) && i < len(opts.restore.globals); i++ {
				gs := opts.restore.globals[i]
				if globalCells[i] == nil {
					continue
				}
				writeGlobalObject(globalCells[i], gs.typ, gs.bits)
				if gs.typ == ValV128 {
					writeGlobalObjectV128(globalCells[i], gs.vec)
				}
			}
		}
		jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
	}

	// Table descriptor: [len u32][max u32][entry...], 32-byte entries
	// {codePtr u64, sigID u32, pad u32, homeLinMem u64, refSlot u64}. homeLinMem is the
	// linear-memory base of the instance the funcref belongs to, so call_indirect
	// runs each entry in its home context (cross-instance funcrefs swap context;
	// same-instance entries take a fast path). Allocate the descriptor even for a
	// zero-length table so call_indirect reads len=0 and traps out-of-bounds.
	var tableDesc []byte
	var funcRefDescs []byte
	if c.HasTable {
		var desc []byte
		var size int
		if c.tableImport != "" {
			// Shared cross-instance table: run on the exporting instance's descriptor.
			t, ok := imports.table(c.tableImport)
			if !ok {
				return nil, fmt.Errorf("missing imported table %q", c.tableImport)
			}
			desc = t.desc
			if len(desc) < 8 {
				return nil, fmt.Errorf("imported table %q descriptor is invalid", c.tableImport)
			}
			size = int(binary.LittleEndian.Uint32(desc))
			cap := int(binary.LittleEndian.Uint32(desc[4:]))
			if cap < size {
				return nil, fmt.Errorf("imported table %q descriptor maximum %d < size %d", c.tableImport, cap, size)
			}
			if size < c.tableImportMin {
				return nil, fmt.Errorf("imported table %q size %d < required minimum %d", c.tableImport, size, c.tableImportMin)
			}
			if c.tableImportHasMax && cap > c.tableImportMax {
				return nil, fmt.Errorf("imported table %q maximum %d > required maximum %d", c.tableImport, cap, c.tableImportMax)
			}
			tableDesc = desc // imported/shared descriptor; re-exportable, not owned
		} else {
			size = c.TableSize
			cap := c.TableMax
			if cap == 0 {
				cap = size
			}
			desc = ar.Alloc(8 + cap*runtime.TableEntryBytes)
			binary.LittleEndian.PutUint32(desc, uint32(size))
			binary.LittleEndian.PutUint32(desc[4:], uint32(cap))
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

		funcRefDescs = ar.Alloc(runtime.TableEntryBytes * (len(c.FuncTypeID) + 1))
		for fidx := 0; fidx < len(c.FuncTypeID); fidx++ {
			off := (fidx + 1) * runtime.TableEntryBytes
			if li := fidx - c.NumImports; li >= 0 && li < len(c.Entry) {
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], uint64(base)+uint64(c.Entry[li]))
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], selfLinMem)
			} else if fidx < c.NumImports {
				if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.localIdx < len(ex.inst.c.Entry) {
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], uint64(ex.inst.base)+uint64(ex.inst.c.Entry[ex.localIdx]))
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], uint64(ex.inst.jm.LinMemBase()))
				} else if addr, ok := thunkAddr[uint32(fidx)]; ok {
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], addr)
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], selfLinMem)
				}
			}
			binary.LittleEndian.PutUint32(funcRefDescs[off+runtime.TableEntrySigIDOffset:], c.FuncTypeID[fidx])
			binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryRefSlotOffset:], uint64(uintptr(unsafe.Pointer(&funcRefDescs[off]))))
			if fidx < c.NumImports {
				// If this funcref names a cross-instance import, keep its first-class
				// reference identity tied to the exporting instance's canonical descriptor.
				if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.inst.funcRefDescs != nil {
					homeFidx := ex.inst.c.NumImports + ex.localIdx
					homeOff := (homeFidx + 1) * runtime.TableEntryBytes
					if homeOff+runtime.TableEntryBytes <= len(ex.inst.funcRefDescs) {
						copy(funcRefDescs[off+runtime.TableEntryRefSlotOffset:off+runtime.TableEntryRefSlotOffset+8], ex.inst.funcRefDescs[homeOff+runtime.TableEntryRefSlotOffset:homeOff+runtime.TableEntryRefSlotOffset+8])
					}
				}
			}
		}
		jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&funcRefDescs[0])), uint32(len(c.FuncTypeID)+1))

		writeTableEntry := func(entry []byte, fidx uint32) {
			if fidx == nullFuncRefIndex {
				clear(entry)
				return
			}
			payload := int(fidx) + 1
			if payload <= 0 || payload >= len(c.FuncTypeID)+1 {
				clear(entry)
				return
			}
			copy(entry, funcRefDescs[payload*runtime.TableEntryBytes:(payload+1)*runtime.TableEntryBytes])
		}
		type resolvedElemInit struct {
			elem ElemInit
			base uint32
		}
		resolvedElems := make([]resolvedElemInit, len(c.Elems))
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
			resolvedElems[seg] = resolvedElemInit{elem: el, base: elemBase}
		}
		for _, init := range resolvedElems {
			for k, fidx := range init.elem.Funcs {
				slot := int(init.base) + k
				off := 8 + slot*runtime.TableEntryBytes
				writeTableEntry(desc[off:off+runtime.TableEntryBytes], fidx)
			}
		}
		if len(c.passiveElems) > 0 {
			edesc := ar.Alloc(runtime.PassiveElemDescBytes * len(c.passiveElems))
			for i, el := range c.passiveElems {
				if len(el.Funcs) == 0 {
					continue
				}
				entries := ar.Alloc(runtime.TableEntryBytes * len(el.Funcs))
				for k, fidx := range el.Funcs {
					writeTableEntry(entries[k*runtime.TableEntryBytes:(k+1)*runtime.TableEntryBytes], fidx)
				}
				off := i * runtime.PassiveElemDescBytes
				binary.LittleEndian.PutUint64(edesc[off:], uint64(uintptr(unsafe.Pointer(&entries[0]))))
				binary.LittleEndian.PutUint32(edesc[off+8:], uint32(len(el.Funcs)))
			}
			jm.SetPassiveElemPtr(uintptr(unsafe.Pointer(&edesc[0])))
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&desc[0])))
	}

	var passiveDataDesc []byte
	if len(c.PassiveData) > 0 {
		// Descriptor layout is shared with the JIT: {ptr u64, len u32, pad u32}.
		// Descriptors are per-instance because data.drop mutates len. Bytes are the
		// immutable compiled-module slices retained by c for the instance lifetime.
		var restoreLens []uint32
		if opts.restore != nil {
			restoreLens = snapshotPassiveDataLens(opts.restore)
			if err := validatePassiveDataLens(c, restoreLens); err != nil {
				return nil, fmt.Errorf("snapshot passive data: %w", err)
			}
		}
		desc := ar.Alloc(runtime.PassiveDataDescBytes * len(c.PassiveData))
		for i, d := range c.PassiveData {
			off := i * runtime.PassiveDataDescBytes
			if len(d.Bytes) != 0 {
				binary.LittleEndian.PutUint64(desc[off:], uint64(uintptr(unsafe.Pointer(&d.Bytes[0]))))
			}
			segLen := uint32(len(d.Bytes))
			if opts.restore != nil {
				segLen = restoreLens[i]
			}
			binary.LittleEndian.PutUint32(desc[off+8:], segLen)
		}
		jm.SetPassiveDataPtr(uintptr(unsafe.Pointer(&desc[0])))
		passiveDataDesc = desc
	}

	if opts.restore != nil {
		// The snapshot's linear-memory bytes already reflect post-data-init state
		// plus every mutation up to the capture point, so copy them wholesale and
		// skip the module's active data segments below.
		dst := jm.HostBytes()
		if len(opts.restore.memory) > len(dst) {
			return nil, fmt.Errorf("snapshot memory (%d bytes) exceeds instance memory (%d bytes)", len(opts.restore.memory), len(dst))
		}
		copy(dst, opts.restore.memory)
	}
	if len(c.Data) > 0 && opts.restore == nil {
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
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, syncMode: c.syncHostImports, ctrl: ctrl, syncHosts: syncHosts, globals: globals, globalCells: globalCells, tableDesc: tableDesc, funcRefDescs: funcRefDescs, passiveDataDesc: passiveDataDesc, thunkMem: thunkMem, gc: collector,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, c.maxResultSlots),
	}
	if in.syncMode {
		in.hostCall = in.newHostDispatch()
	}

	// Run the start function (() -> ()) now that memory, globals, table, and data
	// are initialized. A trap here aborts instantiation. Skip it on a snapshot
	// restore: start already ran in the instance the snapshot was taken from, and
	// its effects are baked into the restored memory/globals.
	if c.HasStart && opts.restore == nil {
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
// host function imports that may be materialized as funcrefs, returning each such
// import's thunk entry address and the mapping (nil when none). We generate for
// every host-bound import in table-using modules, not just imports mentioned by
// active segments: passive/declarative element segments and ref.func can also
// place an import into a table later. A call_indirect through a legacy async
// HostFunc thunk logs the host call for the normal post-invoke replay; a
// sync-mode thunk uses the synchronous control frame and returns the host results
// through the wrapper result slots.
func buildHostFuncThunks(c *Compiled, imports Imports) (map[uint32]uint64, []byte, error) {
	var blob []byte
	offs := map[uint32]int{}
	for fidx := 0; fidx < c.NumImports; fidx++ {
		key := c.Imports[fidx]
		if _, isCross := imports[key].(*InstanceExport); isCross {
			continue // cross-instance funcref, not a host function
		}
		if c.syncHostImports {
			if fidx >= len(c.importFuncSigs) {
				return nil, nil, fmt.Errorf("import %q may become a table funcref but its signature is missing", key)
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
				return nil, nil, fmt.Errorf("import %q may become a table funcref and uses %d param slot(s), %d result slot(s); synchronous table host funcrefs support at most %d slots in each direction", key, paramSlots, resultSlots, runtime.MaxHostArity)
			}
			offs[uint32(fidx)] = len(blob)
			blob = append(blob, railshotHostIndirectSyncThunk(uint32(fidx), paramSlots, resultSlots)...)
			continue
		}
		if _, isHost := imports[key].(HostFunc); !isHost {
			if imports[key] != nil {
				return nil, nil, fmt.Errorf("import %q may become a table funcref but is %T; table host funcrefs in async mode support only legacy wago.HostFunc bindings", key, imports[key])
			}
			continue
		}
		offs[uint32(fidx)] = len(blob)
		blob = append(blob, railshotHostIndirectThunk(uint32(fidx))...)
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
// memory. An imported memory is left for the host to Close. Close is idempotent;
// the error result is always nil today and exists for forward compatibility.
func (in *Instance) Close() error {
	if in == nil || in.closed {
		return nil
	}
	in.closed = true
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
	return nil
}

// resetToSnapshot returns a live instance to the captured state of s in place —
// reloading linear memory, module-local globals, and passive-data drop state —
// without unmapping code or re-acquiring the engine/arena/memory. It backs the
// snapshot pool's fast between-lease reset. The instance must be one this
// snapshot's module produced
// (the pool guarantees it) and must own its memory.
func (in *Instance) resetToSnapshot(s *Snapshot) error {
	if in.c != s.c {
		return errors.New("wago: resetToSnapshot: instance is not from this snapshot's module")
	}
	if !in.ownsMem {
		return errors.New("wago: resetToSnapshot: instance memory is host-imported")
	}
	in.jm.RestoreLinear(s.memory)
	for i := 0; i < len(in.globalCells) && i < len(s.globals); i++ {
		cell := in.globalCells[i]
		if cell == nil || i < len(in.c.GlobalImports) {
			continue // imported globals belong to the host, not the snapshot
		}
		gs := s.globals[i]
		writeGlobalObject(cell, gs.typ, gs.bits)
		if gs.typ == ValV128 {
			writeGlobalObjectV128(cell, gs.vec)
		}
	}
	if len(in.passiveDataDesc) != 0 {
		lens := snapshotPassiveDataLens(s)
		if err := validatePassiveDataLens(in.c, lens); err != nil {
			return fmt.Errorf("wago: resetToSnapshot passive data: %w", err)
		}
		for i, n := range lens {
			off := i*runtime.PassiveDataDescBytes + 8
			binary.LittleEndian.PutUint32(in.passiveDataDesc[off:], n)
		}
	}
	in.ic = [4]invokeCache{} // drop memoized export resolution; state changed underneath
	in.icNext = 0
	return nil
}

// Memory returns the instance's linear-memory object (instance-owned or the
// host-imported one). Use Memory().Bytes() for the zero-copy byte view.
func (in *Instance) Memory() *Memory { return in.memory }

// Imports returns the imports map this instance was created with, for retrieving
// imported objects (e.g. a *Memory or *Global) by "module.name" key. The map is
// the one passed to Instantiate; do not mutate it.
func (in *Instance) Imports() Imports { return in.imports }
