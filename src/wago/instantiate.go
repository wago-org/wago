package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
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
	table                  *Table        // lazily created importer-owned local export-handle chain
	tableDescPtr           uintptr       // local/imported descriptor address; arena/table ownership keeps it live
	tableDescLen           int           // descriptor byte length for safe slice reconstruction
	funcRefDescs           []byte        // canonical funcref descriptor handles for this instance's function index space
	passiveDataDesc        []byte        // per-instance data-segment descriptors; active slots start dropped
	thunkMem               []byte        // executable mapping for host-func-in-table log thunks (nil if none)
	gc                     *gc.Collector // nil for modules with no Wasm GC descriptors/runtime use
	serArgs, results, trap []byte
	resultVals             []uint64       // reusable Invoke result buffer (valid until the next call)
	ic                     [4]invokeCache // tiny fixed export resolution cache
	icNext                 uint8          // round-robin replacement cursor
	refStore               *referenceStore
	lifeMu                 sync.Mutex
	resourceRefs           int
	closed                 bool // logical close; retained references may defer physical release
	resourcesClosed        bool
	nativeControlShared    bool // entered from another instance; prepared control fields may be overwritten

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
	export            string
	valid             bool
	li                int // local index, or -1-import index for an InstanceExport re-export
	paramSlots        int
	resultSlots       int
	hasFuncRefParams  bool
	hasFuncRefResults bool
	resultWide        []bool // one entry per returned uint64 slot; false means read low 32 bits
}

// InstantiateOptions configures instance creation from a *Compiled. When
// instantiating from a *Snapshot both fields are ignored: the snapshot carries
// the imports and GC config it was created with.
type InstantiateOptions struct {
	Imports Imports
	GC      GCConfig
	store   *referenceStore

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
		if err := validateSnapshotModule(s.c); err != nil {
			return nil, err
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
	c, err := c.linkModule(imports, opts.store)
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
	var registeredInstance *Instance
	var hostAttachments hostFuncRefAttachments
	var tableAttachments tableImportAttachments
	var globalAttachments globalImportAttachments
	defer func() {
		if !success {
			hostAttachments.detachAll()
			globalAttachments.detachAll()
			tableAttachments.detachAll()
			if registeredInstance != nil && registeredInstance.refStore != nil {
				registeredInstance.refStore.instanceClosed(registeredInstance)
			}
			if collector != nil {
				collector.Close()
			}
		}
	}()
	for i, key := range c.Imports {
		owner, ok := imports[key].(*HostFuncRef)
		if !ok {
			continue
		}
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("imported host funcref %q has no signature", key)
		}
		if err := hostAttachments.attach(owner, opts.store, c.importFuncSigs[i]); err != nil {
			return nil, fmt.Errorf("imported host funcref %q: %w", key, err)
		}
	}
	importGlobals, err := c.importedGlobals(imports)
	if err != nil {
		return nil, err
	}
	for i, imp := range c.GlobalImports {
		if !isReferenceValType(imp.Type) {
			continue
		}
		if err := globalAttachments.attach(importGlobals[i].global, opts.store); err != nil {
			return nil, fmt.Errorf("imported global %q.%q: %w", imp.Module, imp.Name, err)
		}
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
		guarded, shared := m.importShape()
		if c.boundsMode == BoundsChecksSignalsBased && !guarded {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory %q is not guard-page backed; signals-based bounds checks require a guard-page memory (build with -tags wago_guardpage)", c.memoryImport)
		}
		if shared {
			// Cross-instance shared memory: the importer runs directly on the memory
			// owner's JobMemory, including its fixed negative-offset basedata region.
			// Every per-instance pointer that lives in basedata — the globals array,
			// the table pointer/directory, the host-call ctx (control frame or async
			// log), the funcref descriptor table, and the passive element/data state —
			// is written once at instantiation from THIS instance's arena and is never
			// re-established per call (only the stack fence is refreshed on entry, see
			// callNative). A second importer of the same memory would overwrite those
			// slots, so calls into the first importer read the second's state, and once
			// the second's arena is freed the first dangles into released memory —
			// cross-instance corruption and use-after-free during ordinary sequential
			// instantiation. Until the ABI separates linear-memory backing from
			// per-instance basedata, a shared-memory importer may only compute over the
			// shared linear pages: no globals, tables, funcrefs, host calls, or passive
			// segments (imported or local), all of which claim a basedata slot.
			hasHostCtx := c.syncHostImports || c.needsPublicFuncrefHostReentry()
			if !hasHostCtx {
				for _, key := range c.Imports {
					if _, cross := imports[key].(*InstanceExport); !cross {
						hasHostCtx = true // async/legacy host import installs the host-call log ctx
						break
					}
				}
			}
			// A sole imported table only repoints the shared table slot at storage
			// that is reference-counted (not this instance's arena), so it stays
			// UAF-safe; a local table or any multi-table shape installs an
			// arena-backed descriptor/directory that would dangle for the memory
			// owner and co-importers once this instance's arena is freed.
			hasPrivateTableState := c.tableCount() > 1 || c.tableCount() > c.tableImportCount()
			// Globals need a basedata pointer only when native functions can access
			// them. An initializer-only module may read imported immutable globals
			// while applying active segments without installing per-instance state
			// into the shared memory owner's basedata.
			hasNativeGlobalState := len(c.Globals) > 0 && len(c.Entry) > 0
			if hasNativeGlobalState || hasPrivateTableState || len(c.PassiveData) > 0 ||
				len(c.passiveElems) > 0 || c.needsFuncRefDescs() || hasHostCtx {
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("a module importing a shared memory may not install per-instance basedata state (globals, local or multiple tables, funcrefs, host calls, or passive segments) that would alias the shared linear memory owner's region")
			}
		}
		if err := m.attachImporter(); err != nil {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory %q: %w", c.memoryImport, err)
		}
		jm, memObj = m.jobMemory(), m
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
		memObj, ownsMem = &Memory{jm: jm}, true
	}
	// Release the memory only if this instance owns it; an imported *Memory is the
	// host's, so just release the in-use claim.
	closeMem := func() {
		if ownsMem {
			runtime.ReleaseJobMemory(jm)
		} else {
			memObj.detachImporter()
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
	syncMode := c.syncHostImports || c.needsPublicFuncrefHostReentry()
	if syncMode {
		// Synchronous host-call path: install the control frame (not the async
		// log) as the import ctx. Modules that accept public funcrefs and can call
		// them indirectly also need this frame so an owned host descriptor remains
		// callable after crossing from another instance.
		ctrl = ar.AllocNoZero(runtime.HostCtrlFrameBytes)
		jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
		if len(c.Imports) > 0 {
			syncHosts, err = c.buildSyncHosts(imports)
			if err != nil {
				return nil, fmt.Errorf("instantiate: %w", err)
			}
		}
	} else if len(c.Imports) > 0 {
		hasHostImport := false
		for i, key := range c.Imports {
			if _, cross := imports[key].(*InstanceExport); cross {
				continue
			}
			hasHostImport = true
			if imports[key] == nil {
				return nil, fmt.Errorf("import %q: legacy async host calls require wago.HostFunc or *wago.HostFuncRef", key)
			}
			if i >= len(c.importFuncSigs) {
				return nil, fmt.Errorf("import %q: missing signature", key)
			}
			if _, err := bindHostImport(imports[key], c.importFuncSigs[i]); err != nil {
				return nil, fmt.Errorf("import %q: legacy async host call: %w", key, err)
			}
		}
		if hasHostImport {
			// The log's count header is reset at the start of every Invoke and its
			// body is written by native code before the host reads it, so the ~64 KiB
			// buffer needs no instantiate-time zero-fill.
			hostLog = ar.AllocNoZero(runtime.HostCallLogBytes)
			jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))
		}
	}
	jm.SetStackFence(eng.StackLimit()) // trap runaway recursion instead of faulting

	var initErr error
	var tableDesc []byte
	var funcRefDescs []byte
	var writeTableEntry func([]byte, uint32)
	if c.needsFuncRefDescs() {
		selfLinMem := uint64(jm.LinMemBase())
		var thunkAddr map[uint32]uint64
		needsHostThunk := c.hasFuncrefTable()
		if !needsHostThunk {
			for _, key := range c.Imports {
				if _, owned := imports[key].(*HostFuncRef); owned {
					needsHostThunk = true
					break
				}
			}
		}
		if needsHostThunk {
			// Host functions that can flow through a funcref table need per-instance
			// thunks. An explicitly owned table-free ref.func also needs one because
			// its public token may later enter another instance's table/call_indirect.
			var terr error
			thunkAddr, thunkMem, terr = buildHostFuncThunks(c, imports)
			if terr != nil {
				return nil, terr
			}
		}
		funcRefDescs = ar.Alloc(runtime.TableEntryBytes * (len(c.FuncTypeID) + 1))
		for fidx := 0; fidx < len(c.FuncTypeID); fidx++ {
			off := (fidx + 1) * runtime.TableEntryBytes
			if li := fidx - c.NumImports; li >= 0 && li < len(c.Entry) {
				code, home := uint64(base)+uint64(c.Entry[li]), selfLinMem
				if li < len(c.InternalEntry) && c.InternalEntry[li] != c.Entry[li] && funcSigIntRegABI(c.Funcs[li]) {
					code = uint64(base) + uint64(c.InternalEntry[li])
					home |= uint64(1) << 63
				}
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], code)
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], home)
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
				// Cross-instance imports reuse the producer's canonical identity when
				// that producer already owns a descriptor arena.
				if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.inst.funcRefDescs != nil {
					homeFidx := ex.inst.c.NumImports + ex.localIdx
					homeOff := (homeFidx + 1) * runtime.TableEntryBytes
					if homeOff+runtime.TableEntryBytes <= len(ex.inst.funcRefDescs) {
						copy(funcRefDescs[off+runtime.TableEntryRefSlotOffset:off+runtime.TableEntryRefSlotOffset+8], ex.inst.funcRefDescs[homeOff+runtime.TableEntryRefSlotOffset:homeOff+runtime.TableEntryRefSlotOffset+8])
					}
				}
			}
		}
		jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&funcRefDescs[0])))
		writeTableEntry = func(entry []byte, fidx uint32) {
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
	}
	writeElemEntry := func(entry []byte, refType ValType, value RefInit) error {
		switch normalizedElemRefType(refType) {
		case ValExternRef:
			if !value.Null {
				return fmt.Errorf("externref element contains a non-null initializer")
			}
			clear(entry)
			return nil
		case ValFuncRef:
			if writeTableEntry == nil {
				return fmt.Errorf("funcref element has no descriptor arena")
			}
			if value.Null {
				writeTableEntry(entry, nullFuncRefIndex)
			} else {
				writeTableEntry(entry, value.FuncIndex)
			}
			return nil
		default:
			return fmt.Errorf("unsupported element reference type %s", refType)
		}
	}

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
				if g.HasInitFunc {
					off := (int(g.InitFunc) + 1) * runtime.TableEntryBytes
					if off < runtime.TableEntryBytes || off+runtime.TableEntryBytes > len(funcRefDescs) {
						return nil, fmt.Errorf("global %d ref.func initializer index %d has no descriptor", i, g.InitFunc)
					}
					bits = uint64(uintptr(unsafe.Pointer(&funcRefDescs[off])))
				}
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
		if len(c.Entry) > 0 {
			jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
		}
	}

	// Table descriptors are [len u32][max u32][entry...]. Funcref entries retain
	// their direct 32-byte call descriptor; externref entries are opaque 8-byte
	// handles. Table 0 remains in the direct basedata slot. Multiple local tables
	// also get a compact descriptor-pointer directory; native table-0 code never
	// reads it.
	if c.HasTable {
		tableCount := c.tableCount()
		var tableDir []byte
		if tableCount > 1 {
			tableDir = ar.Alloc(8 * tableCount)
		}
		for tableIndex := 0; tableIndex < tableCount; tableIndex++ {
			var desc []byte
			var size int
			def := c.tableDef(tableIndex)
			entryBytes := c.tableEntryBytes(tableIndex)
			if importDef, imported := c.tableImportAt(tableIndex); imported {
				// Shared cross-instance table: run on the exporting instance's descriptor
				// only after proving exact type and externref-store compatibility. Aliased
				// declarations attach one importer root while each declaration validates.
				t, ok := imports.table(importDef.Key)
				if !ok {
					return nil, fmt.Errorf("missing imported table %q", importDef.Key)
				}
				if err := tableAttachments.attach(t, c.tableElementType(tableIndex), opts.store); err != nil {
					return nil, fmt.Errorf("imported table %q: %w", importDef.Key, err)
				}
				desc = t.desc
				if len(desc) < 8 {
					return nil, fmt.Errorf("imported table %q descriptor is invalid", importDef.Key)
				}
				size = int(binary.LittleEndian.Uint32(desc))
				capacity := int(binary.LittleEndian.Uint32(desc[4:]))
				if capacity < size || 8+capacity*entryBytes > len(desc) {
					return nil, fmt.Errorf("imported table %q descriptor maximum %d < size %d or exceeds storage", importDef.Key, capacity, size)
				}
				if size < importDef.Min {
					return nil, fmt.Errorf("imported table %q size %d < required minimum %d", importDef.Key, size, importDef.Min)
				}
				if importDef.HasMax {
					// The descriptor capacity is only an allocation reservation; a table
					// with no declared maximum still carries a finite reserve. Spec limit
					// matching requires the provided type to actually declare a maximum
					// when the import expects one, so consult the owner's declared bit
					// rather than treating the reservation as the maximum.
					if t.owner == nil || !t.owner.declaredHasMax {
						return nil, fmt.Errorf("imported table %q has no declared maximum but a maximum of %d is required", importDef.Key, importDef.Max)
					}
					if capacity > importDef.Max {
						return nil, fmt.Errorf("imported table %q maximum %d > required maximum %d", importDef.Key, capacity, importDef.Max)
					}
				}
			} else {
				size = def.Size
				capacity := def.Max
				if capacity == 0 {
					capacity = size
				}
				desc = ar.Alloc(8 + capacity*entryBytes)
				binary.LittleEndian.PutUint32(desc, uint32(size))
				binary.LittleEndian.PutUint32(desc[4:], uint32(capacity))
			}
			if def.HasInitFunc {
				if entryBytes != runtime.TableEntryBytes || writeTableEntry == nil {
					return nil, fmt.Errorf("table %d has a funcref initializer with externref storage", tableIndex)
				}
				for slot := 0; slot < size; slot++ {
					off := 8 + slot*entryBytes
					writeTableEntry(desc[off:off+entryBytes], def.InitFunc)
				}
			}
			if tableIndex == 0 {
				tableDesc = desc
			}
			if tableCount > 1 {
				binary.LittleEndian.PutUint64(tableDir[tableIndex*8:], uint64(uintptr(unsafe.Pointer(&desc[0]))))
			}
		}
		for seg, el := range c.Elems {
			desc := tableDesc
			if el.TableIndex != 0 {
				ptr := uintptr(binary.LittleEndian.Uint64(tableDir[int(el.TableIndex)*8:]))
				header := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), 8)
				size := int(binary.LittleEndian.Uint32(header))
				entryBytes := c.tableEntryBytes(int(el.TableIndex))
				desc = unsafe.Slice((*byte)(unsafe.Pointer(ptr)), 8+size*entryBytes)
			}
			size := int(binary.LittleEndian.Uint32(desc))
			elemBase := el.Offset.Base
			if el.Offset.HasGlobal {
				if el.Offset.Global < 0 || el.Offset.Global >= len(c.Globals) || el.Offset.Global >= len(globalCells) || globalCells[el.Offset.Global] == nil {
					initErr = fmt.Errorf("element offset global %d out of range", el.Offset.Global)
					break
				}
				elemBase = uint32(readGlobalObject(globalCells[el.Offset.Global], c.Globals[el.Offset.Global].Type))
			}
			end := uint64(elemBase) + uint64(len(el.Values))
			if end > uint64(size) {
				initErr = fmt.Errorf("active element segment %d out of bounds on table %d: offset %d + length %d > table size %d", seg, el.TableIndex, elemBase, len(el.Values), size)
				break
			}
			entryBytes := c.tableEntryBytes(int(el.TableIndex))
			for k, value := range el.Values {
				slot := int(elemBase) + k
				off := 8 + slot*entryBytes
				if err := writeElemEntry(desc[off:off+entryBytes], el.RefType, value); err != nil {
					initErr = fmt.Errorf("active element segment %d value %d: %w", seg, k, err)
					break
				}
			}
			if initErr != nil {
				break
			}
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&tableDesc[0])))
		if len(tableDir) != 0 {
			jm.SetTableDirPtr(uintptr(unsafe.Pointer(&tableDir[0])))
		}
	}

	if initErr == nil && len(c.passiveElems) > 0 {
		edesc := ar.Alloc(runtime.PassiveElemDescBytes * len(c.passiveElems))
		for i, el := range c.passiveElems {
			if len(el.Values) == 0 {
				continue
			}
			entryBytes := runtime.TableEntryBytes
			if normalizedElemRefType(el.RefType) == ValExternRef {
				entryBytes = 8
			}
			entries := ar.Alloc(entryBytes * len(el.Values))
			for k, value := range el.Values {
				if err := writeElemEntry(entries[k*entryBytes:(k+1)*entryBytes], el.RefType, value); err != nil {
					initErr = fmt.Errorf("passive element segment %d value %d: %w", i, k, err)
					break
				}
			}
			if initErr != nil {
				break
			}
			off := i * runtime.PassiveElemDescBytes
			binary.LittleEndian.PutUint64(edesc[off:], uint64(uintptr(unsafe.Pointer(&entries[0]))))
			binary.LittleEndian.PutUint32(edesc[off+8:], uint32(len(el.Values)))
		}
		jm.SetPassiveElemPtr(uintptr(unsafe.Pointer(&edesc[0])))
	}

	var passiveDataDesc []byte
	if len(c.PassiveData) > 0 {
		// Descriptor layout is shared with the JIT: {ptr u64, len u32, pad u32}.
		// Descriptors are per-instance because data.drop mutates len. Passive bytes
		// are retained by c; active slots have nil bytes and start at length zero.
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
	if initErr == nil && len(c.Data) > 0 && opts.restore == nil {
		lin := jm.CurrentBytes() // active data must fit the initial size, not the reservation
		for seg, d := range c.Data {
			off := d.Offset.Base
			if d.Offset.HasGlobal {
				if d.Offset.Global < 0 || d.Offset.Global >= len(c.Globals) || d.Offset.Global >= len(globalCells) || globalCells[d.Offset.Global] == nil {
					initErr = fmt.Errorf("data offset global %d out of range", d.Offset.Global)
					break
				}
				off = uint32(readGlobalObject(globalCells[d.Offset.Global], c.Globals[d.Offset.Global].Type))
			}
			end := uint64(off) + uint64(len(d.Bytes))
			if end > uint64(len(lin)) {
				initErr = fmt.Errorf("active data segment %d out of bounds: offset %d + length %d > memory size %d", seg, off, len(d.Bytes), len(lin))
				break
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
	if err := jm.BindTrapCell(trap); err != nil {
		return nil, fmt.Errorf("bind trap cell: %w", err)
	}

	var tableDescPtr uintptr
	if len(tableDesc) != 0 {
		tableDescPtr = uintptr(unsafe.Pointer(&tableDesc[0]))
	}
	in := &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, syncMode: syncMode, ctrl: ctrl, syncHosts: syncHosts, globals: globals, globalCells: globalCells, tableDescPtr: tableDescPtr, tableDescLen: len(tableDesc), funcRefDescs: funcRefDescs, passiveDataDesc: passiveDataDesc, thunkMem: thunkMem, gc: collector,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, c.maxResultSlots),
	}
	registeredInstance = in
	if opts.store != nil {
		if err := opts.store.registerInstance(in); err != nil {
			return nil, err
		}
		in.refStore = opts.store
	}
	if in.syncMode {
		in.hostCall = in.newHostDispatch()
	}

	if initErr != nil {
		if retainProducerRootsInImportedTables(in) {
			success = true
			_ = in.Close()
		}
		return nil, initErr
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
			var startErr error
			if in.syncMode {
				startErr = in.callNativeSync(startEntry)
			} else {
				startErr = callNative(c, eng, jm, false, startEntry, serArgs, trap, results)
			}
			if startErr != nil {
				// Instantiation writes to imported tables are store side effects. If a
				// local funcref remains installed when start traps, the shared table
				// becomes the failed instance's lifetime owner. The table prunes roots
				// no longer present in any slot, so retention stays bounded by its
				// descriptor capacity rather than by failed-instantiation count.
				if retainProducerRootsInImportedTables(in) {
					success = true
					_ = in.Close()
				}
				return nil, fmt.Errorf("start function trapped: %w", startErr)
			}
		}
	}

	success = true
	return in, nil
}

func (c *Compiled) needsPublicFuncrefHostReentry() bool {
	if c == nil || !c.hasFuncrefTable() {
		return false
	}
	for _, sig := range c.Funcs {
		if hasValType(sig.Params, ValFuncRef) {
			return true
		}
	}
	return false
}

func funcSigIntRegABI(sig FuncSig) bool {
	if len(sig.Results) > 1 || len(sig.Params) > 8 {
		return false
	}
	for _, t := range append(append([]ValType{}, sig.Params...), sig.Results...) {
		if t != ValI32 && t != ValI64 {
			return false
		}
	}
	return true
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
			dispatch := uint32(fidx)
			owned := false
			if owner, ok := imports[key].(*HostFuncRef); ok && owner != nil {
				owner.mu.Lock()
				dispatch = hostFuncRefDispatchBit | owner.dispatchIndex
				owner.mu.Unlock()
				owned = true
			}
			offs[uint32(fidx)] = len(blob)
			if owned {
				blob = append(blob, railshotHostIndirectOwnedSyncThunk(dispatch, paramSlots, resultSlots)...)
			} else {
				blob = append(blob, railshotHostIndirectSyncThunk(dispatch, paramSlots, resultSlots)...)
			}
			continue
		}
		switch imports[key].(type) {
		case HostFunc, *HostFuncRef:
			offs[uint32(fidx)] = len(blob)
			blob = append(blob, railshotHostIndirectThunk(uint32(fidx))...)
		default:
			if imports[key] != nil {
				return nil, nil, fmt.Errorf("import %q may become a table funcref but is %T; table host funcrefs in async mode support wago.HostFunc or *wago.HostFuncRef bindings", key, imports[key])
			}
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
// memory. An imported memory is left for the host to Close. Close is idempotent;
// the error result is always nil today and exists for forward compatibility.
func (in *Instance) Close() error {
	if in == nil {
		return nil
	}
	in.lifeMu.Lock()
	alreadyClosed := in.closed
	in.lifeMu.Unlock()
	if alreadyClosed {
		return nil
	}

	// Before marking the instance closed, transfer producer roots to any imported
	// funcref table or global that still holds a local funcref this instance wrote
	// (via table.set/fill/grow/init or global.set). The descriptor embeds this
	// instance's code pointer and home linear-memory address, so it must outlive
	// the write for other importers that later read it. retainResourceRoot refuses
	// a closed instance, so this runs before in.closed is set; the container drops
	// the root when the descriptor is overwritten or the container closes.
	retainProducerRootsInImportedTables(in)
	retainProducerRootsInImportedGlobals(in)

	in.lifeMu.Lock()
	if in.closed {
		in.lifeMu.Unlock()
		return nil
	}
	in.closed = true
	shouldRelease := in.resourceRefs == 0
	store := in.refStore
	in.lifeMu.Unlock()

	detachImportedHostFuncRefs(in)
	detachImportedGlobals(in)
	detachImportedTables(in)
	if store != nil {
		store.instanceClosed(in)
	}
	if shouldRelease {
		in.releaseResources()
	}
	return nil
}

func (in *Instance) releaseResources() {
	in.lifeMu.Lock()
	if in.resourcesClosed {
		in.lifeMu.Unlock()
		return
	}
	in.resourcesClosed = true
	in.lifeMu.Unlock()

	if in.gc != nil {
		in.gc.Close()
	}
	for table := in.table; table != nil; table = table.next {
		table.releaseRetainedInstances()
	}
	if in.thunkMem != nil {
		runtime.Unmap(in.thunkMem)
		in.thunkMem = nil
	}
	in.c.releaseCode()
	runtime.ReleaseArena(in.ar)
	if in.ownsMem {
		if in.memory != nil {
			in.memory.ownerClosed()
		}
		runtime.ReleaseJobMemory(in.jm)
	} else if in.memory != nil {
		in.memory.detachImporter()
	}
	runtime.ReleaseEngine(in.eng)
}

// resetToSnapshot returns a live instance to the captured state of s in place —
// reloading linear memory, module-local globals, and passive-data drop state —
// without unmapping code or re-acquiring the engine/arena/memory. It backs the
// snapshot pool's fast between-lease reset. The instance must be one this
// snapshot's module produced
// (the pool guarantees it) and must own its memory.
func (in *Instance) resetToSnapshot(s *Snapshot) error {
	if s == nil || s.c == nil {
		return errors.New("wago: resetToSnapshot: snapshot has no bound module")
	}
	if err := validateSnapshotModule(s.c); err != nil {
		return err
	}
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
		return fmt.Errorf("reference global is nil")
	}
	if a.set.contains(global) {
		return global.validateReferenceImport(store)
	}
	if err := global.attachReferenceImporter(store); err != nil {
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
		if !isReferenceValType(imp.Type) {
			continue
		}
		provided, ok := in.imports.global(imp.Module + "." + imp.Name)
		if !ok || provided.Global == nil {
			continue
		}
		if seen.add(provided.Global) {
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
			retained = true
		}
	}
	return retained
}

type tableImportAttachments struct {
	set importDedup[*Table]
}

func (a *tableImportAttachments) attach(table *Table, elementType ValType, store *referenceStore) error {
	if err := table.validateImport(elementType, store); err != nil {
		return err
	}
	if a.set.contains(table) {
		return nil
	}
	if err := table.attachImporter(elementType, store); err != nil {
		return err
	}
	a.set.push(table)
	return nil
}

func (a *tableImportAttachments) detachAll() {
	a.set.each((*Table).detachImporter)
	a.set.reset()
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
		if seen.add(table) {
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
			retained = true
		}
	}
	return retained
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
		return unsafe.Slice((*byte)(unsafe.Pointer(in.tableDescPtr)), in.tableDescLen)
	}
	dirPtr := in.jm.TableDirPtr()
	if dirPtr == 0 {
		return nil
	}
	dir := unsafe.Slice((*byte)(unsafe.Pointer(dirPtr)), 8*in.c.tableCount())
	descPtr := uintptr(binary.LittleEndian.Uint64(dir[index*8:]))
	if descPtr == 0 {
		return nil
	}
	def := in.c.tableDef(index)
	capacity := def.Max
	if capacity == 0 {
		capacity = def.Size
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(descPtr)), 8+capacity*in.c.tableEntryBytes(index))
}

// Imports returns the imports map this instance was created with, for retrieving
// imported objects (e.g. a *Memory or *Global) by "module.name" key. The map is
// the one passed to Instantiate; do not mutate it.
func (in *Instance) Imports() Imports { return in.imports }
