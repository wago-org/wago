package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

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
	restore       *Snapshot
	runtime       *Runtime
	origin        InstantiateOrigin
	pluginGC      *GCConfig
	forceSyncHost bool
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

// instanceBuilder owns the pre-commit state of one instantiation. Concrete
// fields keep unsafe/off-heap ownership visible; no generic cleanup stack is
// used on this allocation-sensitive path.
type instanceBuilder struct {
	c       *Compiled
	opts    InstantiateOptions
	imports Imports

	collector           *gc.Collector
	success             bool
	registeredInstance  *Instance
	functionAttachments functionImportAttachments
	hostAttachments     hostFuncRefAttachments
	tableAttachments    tableImportAttachments
	globalAttachments   globalImportAttachments
	tagAttachments      tagImportAttachments
	restoreMemories     []memorySnap
}

// instantiateCore maps code and applies explicit instance options. It is the
// shared engine behind Instantiate for both the compiled and snapshot paths.
func instantiateCore(c *Compiled, opts InstantiateOptions) (*Instance, error) {
	if c == nil {
		return nil, errors.New("wago: instantiate: nil compiled module")
	}
	restoreMemories := snapshotMemories(opts.restore)
	if opts.restore != nil {
		if err := validateSnapshotMemories(c, restoreMemories); err != nil {
			return nil, fmt.Errorf("snapshot memories: %w", err)
		}
	}
	if err := c.preflightImportBindings(opts.Imports); err != nil {
		return nil, err
	}
	b := instanceBuilder{c: c, opts: opts, imports: opts.Imports, restoreMemories: restoreMemories}
	return b.instantiate()
}

func (b *instanceBuilder) validateCompiled() error {
	if err := b.c.validateImportBindings(b.imports, b.opts.store); err != nil {
		return err
	}
	return b.c.validateCached()
}

func (c *Compiled) arenaNeedForImports(imports Imports, syncMode bool) int {
	need := c.instantiateArenaNeed
	if len(c.Imports) == 0 {
		return need
	}
	baselineHostBytes := runtime.HostCallLogBytes
	if c.needsPublicFuncrefHostReentry() || c.usesGCStructHelpers() || c.usesGCArrayHelpers() {
		baselineHostBytes = runtime.HostCtrlFrameBytes
	}
	actualHostBytes := 0
	if syncMode {
		actualHostBytes = runtime.HostCtrlFrameBytes
	} else {
		for _, key := range c.Imports {
			if _, cross := imports[key].(*InstanceExport); !cross {
				actualHostBytes = runtime.HostCallLogBytes
				break
			}
		}
	}
	return need - baselineHostBytes + actualHostBytes
}

func (b *instanceBuilder) prepareCollector() error {
	if !gc.HasHeapObjectTypes(b.c.GCTypeDescs) || b.c.collectorFreeStructuralMetadata() || b.c.collectorFreeGCArrayMetadata() {
		return nil
	}
	collector, err := gc.NewCollector(b.opts.GC, b.c.GCTypeDescs)
	if err != nil {
		return err
	}
	b.collector = collector
	return nil
}

func (b *instanceBuilder) attachImports() ([]*resolvedGlobalImport, error) {
	for i, key := range b.c.Imports {
		switch value := b.imports[key].(type) {
		case *InstanceExport:
			if err := b.functionAttachments.attach(value); err != nil {
				return nil, fmt.Errorf("imported function %q: %w", key, err)
			}
		case *HostFuncRef:
			if i >= len(b.c.importFuncSigs) {
				return nil, fmt.Errorf("imported host funcref %q has no signature", key)
			}
			if err := b.hostAttachments.attach(value, b.opts.store, b.c.importFuncSigs[i]); err != nil {
				return nil, fmt.Errorf("imported host funcref %q: %w", key, err)
			}
		}
	}
	importGlobals, err := b.c.importedGlobals(b.imports)
	if err != nil {
		return nil, err
	}
	for i, imp := range b.c.GlobalImports {
		global := importGlobals[i].global
		if global == nil || (!isReferenceValType(imp.Type) && global.owner == nil) {
			continue
		}
		if err := b.globalAttachments.attach(global, b.opts.store); err != nil {
			return nil, fmt.Errorf("imported global %q.%q: %w", imp.Module, imp.Name, err)
		}
	}
	if b.c.memoryDir != nil {
		for _, def := range b.c.memoryDir.ehTags {
			if def.ImportKey == "" {
				continue
			}
			tag, ok := b.imports.tag(def.ImportKey)
			if !ok {
				return nil, fmt.Errorf("imported tag %q must be an instance-exported *wago.Tag", def.ImportKey)
			}
			if err := b.tagAttachments.attach(tag, def.TypeIndex, b.c.Types); err != nil {
				return nil, fmt.Errorf("imported tag %q: %w", def.ImportKey, err)
			}
		}
	}
	return importGlobals, nil
}

func (b *instanceBuilder) rollbackPreparedState() {
	b.functionAttachments.detachAll()
	b.hostAttachments.detachAll()
	b.globalAttachments.detachAll()
	b.tableAttachments.detachAll()
	b.tagAttachments.detachAll()
	if b.registeredInstance != nil && b.registeredInstance.refStore != nil {
		b.registeredInstance.refStore.instanceClosed(b.registeredInstance)
	}
	if b.collector != nil {
		b.collector.Close()
	}
}

func (b *instanceBuilder) instantiate() (result *Instance, err error) {
	if err := b.validateCompiled(); err != nil {
		return nil, err
	}
	if err := b.prepareCollector(); err != nil {
		return nil, err
	}
	c, opts, imports := b.c, b.opts, b.imports
	restoreMemories := b.restoreMemories
	syncMode := c.importsRequireSync(imports, opts.forceSyncHost)
	defer func() {
		if !b.success {
			b.rollbackPreparedState()
		}
	}()
	importGlobals, err := b.attachImports()
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
		jm         *runtime.JobMemory
		memObj     *Memory
		ownsMem    bool
		memoryObjs []*Memory
		memoryOwns []bool
	)
	var memoryAttachments importDedup[*Memory]
	attachMemory := func(memory *Memory) error {
		if memoryAttachments.contains(memory) {
			return nil
		}
		if err := memory.attachImporter(); err != nil {
			return err
		}
		memoryAttachments.push(memory)
		return nil
	}
	if c.memoryImport != "" {
		m, ok := imports.memory(c.memoryImport)
		if !ok {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("missing imported memory %q", c.memoryImport)
		}
		if def, ok := c.memoryImportAt(0); ok {
			if err := m.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64); err != nil {
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("imported memory %q limits: %w", c.memoryImport, err)
			}
		}
		// A signals-based module elides inline bounds checks and relies on the
		// guard-page fault, so the imported memory must be guard-page backed. Host
		// NewMemory and guard-page instance owners provide one only in a
		// wago_guardpage build; reject a plain mapping (e.g. an explicit-bounds
		// owner's memory, or a deserialized signals-based module in a default binary).
		guarded, _ := m.importShape()
		if c.boundsMode == BoundsChecksSignalsBased && !guarded {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory %q is not guard-page backed; signals-based bounds checks require a guard-page memory (build with -tags wago_guardpage)", c.memoryImport)
		}
		if err := attachMemory(m); err != nil {
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("imported memory %q: %w", c.memoryImport, err)
		}
		jm, memObj = m.jobMemory(), m
	} else {
		initialBytes, maxBytes := c.memorySizeBytes()
		// Restoring from a snapshot: size the fresh mapping to the snapshot's
		// (possibly grown) linear-memory size so the saved bytes fit and memory.size
		// reports the captured value, not the module's declared minimum.
		if len(restoreMemories) != 0 {
			if rb := int(restoreMemories[0].pages) * 65536; rb > initialBytes {
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
	memoryCount := c.memoryCount()
	if memoryCount > 1 {
		memoryObjs = make([]*Memory, memoryCount)
		memoryOwns = make([]bool, memoryCount)
		memoryObjs[0], memoryOwns[0] = memObj, ownsMem
	}
	// Release every owned mapping once and detach every distinct imported memory
	// once. Multiple import declarations may deliberately alias one host Memory.
	closeMem := func() {
		if memoryCount <= 1 {
			if ownsMem {
				runtime.ReleaseJobMemory(jm)
			}
			memoryAttachments.each((*Memory).detachImporter)
			return
		}
		for i := memoryCount - 1; i >= 0; i-- {
			memory := memoryObjs[i]
			if memory != nil && memoryOwns[i] {
				runtime.ReleaseJobMemory(memory.jobMemory())
			}
		}
		memoryAttachments.each((*Memory).detachImporter)
	}
	for i := 1; i < memoryCount; i++ {
		def := c.memoryDef(i)
		if def.ImportKey != "" {
			memory, ok := imports.memory(def.ImportKey)
			if !ok {
				closeMem()
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("missing imported memory %q", def.ImportKey)
			}
			if err := memory.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64); err != nil {
				closeMem()
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("imported memory %q limits: %w", def.ImportKey, err)
			}
			if err := attachMemory(memory); err != nil {
				closeMem()
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("imported memory %q: %w", def.ImportKey, err)
			}
			memoryObjs[i] = memory
			continue
		}
		maxPages := uint64(65535)
		if def.HasMax {
			maxPages = def.Max
		}
		initialPages := def.Min
		if i < len(restoreMemories) && uint64(restoreMemories[i].pages) > initialPages {
			initialPages = uint64(restoreMemories[i].pages)
		}
		secondaryJM, allocErr := runtime.AcquireJobMemoryGrowable(int(initialPages)*65536, int(maxPages)*65536)
		if allocErr != nil {
			closeMem()
			runtime.ReleaseEngine(eng)
			return nil, fmt.Errorf("memory %d: %w", i, allocErr)
		}
		memoryObjs[i] = &Memory{jm: secondaryJM}
		memoryOwns[i] = true
	}
	var nativeMemoryDir []byte
	var nativeTagIDs []byte
	ar, err := runtime.AcquireArena(c.arenaNeedForImports(imports, syncMode))
	if err != nil {
		closeMem()
		runtime.ReleaseEngine(eng)
		return nil, err
	}
	nativeContext := ar.AllocNoZero(runtime.InstanceContextBytes)
	nativeContextPtr := uintptr(unsafe.Pointer(&nativeContext[0]))
	if memoryCount > 1 {
		nativeMemoryDir = ar.Alloc(memoryCount * 16)
		for i, memory := range memoryObjs {
			memoryJM := memory.jobMemory()
			if memoryJM == nil {
				runtime.ReleaseArena(ar)
				closeMem()
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("memory %d owner closed during instantiation", i)
			}
			entry := nativeMemoryDir[i*16:]
			binary.LittleEndian.PutUint64(entry, uint64(memoryJM.LinMemBase()))
			binary.LittleEndian.PutUint32(entry[8:], uint32(len(memoryJM.HostBytes())))
			binary.LittleEndian.PutUint32(entry[12:], memoryJM.CurrentPages())
		}
		jm.SetMemoryDirPtr(uintptr(unsafe.Pointer(&nativeMemoryDir[0])))
	}
	if c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		nativeTagIDs = ar.Alloc(len(c.memoryDir.ehTags) * 8)
		for i, def := range c.memoryDir.ehTags {
			identity := uint64(uintptr(unsafe.Pointer(&nativeTagIDs[i*8])))
			if def.ImportKey != "" {
				tag, ok := imports.tag(def.ImportKey)
				if !ok {
					runtime.ReleaseArena(ar)
					closeMem()
					runtime.ReleaseEngine(eng)
					return nil, fmt.Errorf("imported tag %q is unavailable during native identity setup", def.ImportKey)
				}
				identity = tag.identityValue()
			}
			binary.LittleEndian.PutUint64(nativeTagIDs[i*8:], identity)
		}
		jm.SetEHTagDirPtr(uintptr(unsafe.Pointer(&nativeTagIDs[0])))
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
		if b.success {
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
	if syncMode {
		// Synchronous host-call path: install the control frame (not the async
		// log) as the import ctx. Modules that accept public funcrefs and can call
		// them indirectly also need this frame so an owned host descriptor remains
		// callable after crossing from another instance.
		ctrl = ar.AllocNoZero(runtime.HostCtrlFrameBytes)
		if err := runtime.InitHostCtrlFrame(ctrl); err != nil {
			return nil, fmt.Errorf("instantiate: initialize host control frame: %w", err)
		}
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

	thunkAddr, generatedThunks, err := buildHostFuncThunks(c, imports, syncMode)
	if err != nil {
		return nil, err
	}
	thunkMem = generatedThunks
	if c.dynamicImports && c.NumImports > 0 {
		dispatch := ar.Alloc(c.NumImports * runtime.ImportDispatchEntryBytes)
		selfLinMem := uint64(jm.LinMemBase())
		for i, key := range c.Imports {
			off := i * runtime.ImportDispatchEntryBytes
			if ex, ok := imports[key].(*InstanceExport); ok && ex != nil && ex.inst != nil {
				if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
					return nil, fmt.Errorf("cross-instance import %q references an unavailable function", key)
				}
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCodePtrOffset:], uint64(ex.inst.base)+uint64(ex.inst.c.Entry[ex.localIdx]))
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchHomeLinMemOffset:], uint64(ex.inst.jm.LinMemBase()))
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchTargetContextOffset:], uint64(ex.inst.nativeContext))
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCallerContextOffset:], uint64(nativeContextPtr))
				continue
			}
			addr, ok := thunkAddr[uint32(i)]
			if !ok {
				return nil, fmt.Errorf("import %q has no host dispatch thunk", key)
			}
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCodePtrOffset:], addr)
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchHomeLinMemOffset:], selfLinMem)
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchTargetContextOffset:], uint64(nativeContextPtr))
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCallerContextOffset:], uint64(nativeContextPtr))
		}
		jm.SetImportDispatchPtr(uintptr(unsafe.Pointer(&dispatch[0])))
	}

	var initErr error
	var tableDesc []byte
	var funcRefDescs []byte
	var writeTableEntry func([]byte, uint32)
	if c.needsFuncRefDescs() {
		selfLinMem := uint64(jm.LinMemBase())
		funcRefDescs = ar.Alloc(runtime.FuncRefDescBytes * (len(c.FuncTypeID) + 1))
		binary.LittleEndian.PutUint64(funcRefDescs[runtime.FuncRefContextOffset:], uint64(nativeContextPtr))
		for fidx := 0; fidx < len(c.FuncTypeID); fidx++ {
			off := (fidx + 1) * runtime.FuncRefDescBytes
			targetContext := uint64(nativeContextPtr)
			if li := fidx - c.NumImports; li >= 0 && li < len(c.Entry) {
				code, home := uint64(base)+uint64(c.Entry[li]), selfLinMem
				stagedTailRegABI := c.stagedFeatures().IsEnabled(CoreFeatureTailCall) && (funcSigLocalRegABI(c.Funcs[li]) || funcSigReferenceResultRegABI(c.Funcs[li]))
				if li < len(c.InternalEntry) && c.InternalEntry[li] != c.Entry[li] && (funcSigIntRegABI(c.Funcs[li]) || stagedTailRegABI) {
					code = uint64(base) + uint64(c.InternalEntry[li])
					home |= abi.FuncRefInternalHomeTag
				} else {
					home |= abi.FuncRefLocalWrapperHomeTag
				}
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], code)
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], home)
			} else if fidx < c.NumImports {
				if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.localIdx < len(ex.inst.c.Entry) {
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], uint64(ex.inst.base)+uint64(ex.inst.c.Entry[ex.localIdx]))
					home := uint64(ex.inst.jm.LinMemBase())
					if fidx < len(c.importFuncSigs) && funcSigIntRegABI(c.importFuncSigs[fidx]) {
						home |= abi.FuncRefCrossInstanceHomeTag
					}
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], home)
					targetContext = uint64(ex.inst.nativeContext)
				} else if addr, ok := thunkAddr[uint32(fidx)]; ok {
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], addr)
					binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], selfLinMem)
				}
			}
			binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntrySigKeyOffset:], c.funcTypeKey(fidx))
			binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryRefSlotOffset:], uint64(uintptr(unsafe.Pointer(&funcRefDescs[off]))))
			binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.FuncRefContextOffset:], targetContext)
			if fidx < c.NumImports {
				// Cross-instance imports reuse the producer's canonical identity when
				// that producer already owns a descriptor arena.
				if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.inst.funcRefDescs != nil {
					homeFidx := ex.inst.c.NumImports + ex.localIdx
					homeOff := (homeFidx + 1) * runtime.FuncRefDescBytes
					if homeOff+runtime.FuncRefDescBytes <= len(ex.inst.funcRefDescs) {
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
			off := payload * runtime.FuncRefDescBytes
			copy(entry, funcRefDescs[off:off+runtime.TableEntryBytes])
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
		case ValI31Ref:
			if value.Null {
				clear(entry)
				return nil
			}
			if value.FuncIndex&1 == 0 || len(entry) < 8 {
				return fmt.Errorf("i31 element contains an invalid immediate")
			}
			binary.LittleEndian.PutUint64(entry, uint64(value.FuncIndex))
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
	var gcGlobalRoots [2]gcGlobalRootMapping
	var gcGlobalRootCount uint8
	globalCells := make([]*Global, len(c.Globals))
	if len(c.Globals) > 0 {
		globals = ar.Alloc(8 * len(c.Globals))
		// One heap allocation backs every module-local global cell (a *Global into
		// this slab) instead of one allocation per global; imported globals keep
		// their own cached *Global.
		localCells := make([]Global, len(c.Globals))
		// Wasm global indexes are stored in order in a pointer table: imported
		// global objects first, followed by module-local cells initialized from
		// literal bits, earlier immutable globals, or extended const expressions.
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
				if gcInit, ok := c.gcStructGlobalInit(i); ok {
					if int(gcGlobalRootCount) >= len(gcGlobalRoots) {
						return nil, fmt.Errorf("global %d exceeds staged GC root mapping bound", i)
					}
					ref, slot, err := instantiateGCStructGlobal(b.collector, c.GCTypeDescs, gcInit)
					if err != nil {
						return nil, fmt.Errorf("global %d GC struct initializer: %w", i, err)
					}
					bits = uint64(ref)
					gcGlobalRoots[gcGlobalRootCount] = gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot}
					gcGlobalRootCount++
				} else if gcInit, ok := c.gcArrayGlobalInit(i); ok {
					if int(gcGlobalRootCount) >= len(gcGlobalRoots) {
						return nil, fmt.Errorf("global %d exceeds staged GC root mapping bound", i)
					}
					ref, slot, err := instantiateGCArrayGlobal(b.collector, c.GCTypeDescs, gcInit)
					if err != nil {
						return nil, fmt.Errorf("global %d GC array initializer: %w", i, err)
					}
					bits = uint64(ref)
					gcGlobalRoots[gcGlobalRootCount] = gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot}
					gcGlobalRootCount++
				}
				if g.HasInitFunc {
					off := (int(g.InitFunc) + 1) * runtime.FuncRefDescBytes
					if off < runtime.FuncRefDescBytes || off+runtime.FuncRefDescBytes > len(funcRefDescs) {
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
				if len(g.InitExpr) != 0 {
					value, err := evalCompiledScalarConstExpr(g.InitExpr, g.Type, globalCells, c.Globals, i)
					if err != nil {
						return nil, fmt.Errorf("global %d extended initializer: %w", i, err)
					}
					bits = value
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

	var gcRefTestTable *gcRefTestTableState
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
				exact, err := c.tableExactType(tableIndex)
				if err != nil {
					return nil, fmt.Errorf("imported table %q exact type: %w", importDef.Key, err)
				}
				if err := b.tableAttachments.attach(t, c.tableElementType(tableIndex), exact, c.Types, opts.store, def.Addr64); err != nil {
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
				capacity := c.tableRuntimeCapacity(tableIndex)
				desc = ar.Alloc(8 + capacity*entryBytes)
				binary.LittleEndian.PutUint32(desc, uint32(size))
				binary.LittleEndian.PutUint32(desc[4:], uint32(capacity))
			}
			if def.HasInitFunc {
				if entryBytes != runtime.TableEntryBytes || writeTableEntry == nil {
					return nil, fmt.Errorf("table %d has a funcref initializer with compact-reference storage", tableIndex)
				}
				for slot := 0; slot < size; slot++ {
					off := 8 + slot*entryBytes
					writeTableEntry(desc[off:off+entryBytes], def.InitFunc)
				}
			}
			if init := c.memoryDir.gcI31TableInit; init != nil && int(init.TableIndex) == tableIndex {
				if entryBytes != 8 || int(init.GlobalIndex) >= len(globalCells) || int(init.GlobalIndex) >= len(c.Globals) || globalCells[init.GlobalIndex] == nil || c.Globals[init.GlobalIndex].Type != ValI32 {
					return nil, fmt.Errorf("table %d has an invalid staged i31 initializer", tableIndex)
				}
				bits := uint64(uint32(readGlobalObject(globalCells[init.GlobalIndex], ValI32))<<1 | 1)
				for slot := 0; slot < size; slot++ {
					off := 8 + slot*entryBytes
					binary.LittleEndian.PutUint64(desc[off:off+entryBytes], bits)
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
				header := unsafe.Slice((*byte)(offHeapPtr(ptr)), 8)
				size := int(binary.LittleEndian.Uint32(header))
				entryBytes := c.tableEntryBytes(int(el.TableIndex))
				desc = unsafe.Slice((*byte)(offHeapPtr(ptr)), 8+size*entryBytes)
			}
			size := int(binary.LittleEndian.Uint32(desc))
			table64 := c.tableDef(int(el.TableIndex)).Addr64
			elemBase := uint64(el.Offset.Base)
			if el.Offset.HasGlobal {
				if el.Offset.Global < 0 || el.Offset.Global >= len(c.Globals) || el.Offset.Global >= len(globalCells) || globalCells[el.Offset.Global] == nil {
					initErr = fmt.Errorf("element offset global %d out of range", el.Offset.Global)
					break
				}
				value := readGlobalObject(globalCells[el.Offset.Global], c.Globals[el.Offset.Global].Type)
				if table64 {
					elemBase = value
				} else {
					elemBase = uint64(uint32(value))
				}
			}
			if len(el.Offset.Expr) != 0 {
				offsetType := ValI32
				if table64 {
					offsetType = ValI64
				}
				value, err := evalCompiledScalarConstExpr(el.Offset.Expr, offsetType, globalCells, c.Globals, len(importGlobals))
				if err != nil {
					initErr = fmt.Errorf("element offset extended expression: %w", err)
					break
				}
				if table64 {
					elemBase = value
				} else {
					elemBase = uint64(uint32(value))
				}
			}
			end := elemBase + uint64(len(el.Values))
			if end < elemBase || end > uint64(size) {
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
		if product := c.stagedGCStructProduct(); initErr == nil && (product == stagedGCStructRefTestTable || product == stagedGCStructRefTestConcrete) {
			if c.tableCount() != 1 || c.tableEntryBytes(0) != 8 {
				initErr = fmt.Errorf("GC ref.test product requires one compact local table")
			} else {
				gcRefTestTable, initErr = newGCRefTestTableState(b.collector, tableDesc, product.refTestCanonicalTypes())
			}
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&tableDesc[0])))
		if len(tableDir) != 0 {
			jm.SetTableDirPtr(uintptr(unsafe.Pointer(&tableDir[0])))
		}
	}

	var gcArrayElements *gcArrayElementState
	if initErr == nil && len(c.passiveElems) > 0 {
		edesc := ar.Alloc(runtime.PassiveElemDescBytes * len(c.passiveElems))
		for i, el := range c.passiveElems {
			if len(el.Values) == 0 {
				continue
			}
			entryBytes := elemEntryBytes(el.RefType)
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
		if initErr == nil && c.memoryDir != nil && c.memoryDir.gcArrayElement != nil {
			seg := int(c.memoryDir.gcArrayElement.SegmentIndex)
			if seg < 0 || seg >= len(c.passiveElems) {
				initErr = fmt.Errorf("GC array element segment %d has no descriptor", seg)
			} else {
				desc := edesc[seg*runtime.PassiveElemDescBytes : (seg+1)*runtime.PassiveElemDescBytes]
				gcArrayElements, initErr = instantiateGCArrayElementSegment(b.collector, c.GCTypeDescs, c.memoryDir.gcArrayElement, desc)
			}
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
		// Snapshot memory images already reflect post-data-init state plus every
		// mutation up to capture. Blob-loaded images may trim zero tails; fresh
		// mappings are zeroed, so copying the stored prefixes restores them exactly.
		for i, memory := range restoreMemories {
			memoryJM := jm
			if i != 0 {
				memoryJM = memoryObjs[i].jobMemory()
			}
			dst, hostErr := memoryJM.HostBytesChecked()
			if hostErr != nil {
				return nil, fmt.Errorf("snapshot memory %d host access: %w", i, hostErr)
			}
			if len(memory.image) > len(dst) {
				return nil, fmt.Errorf("snapshot memory %d image (%d bytes) exceeds instance memory (%d bytes)", i, len(memory.image), len(dst))
			}
			copy(dst, memory.image)
		}
	}
	if initErr == nil && len(c.Data) > 0 && opts.restore == nil {
		for seg, d := range c.Data {
			dataJM := jm
			if d.MemoryIndex != 0 {
				if int(d.MemoryIndex) >= len(memoryObjs) || memoryObjs[d.MemoryIndex] == nil {
					initErr = fmt.Errorf("active data segment %d memory index %d is unavailable", seg, d.MemoryIndex)
					break
				}
				dataJM = memoryObjs[d.MemoryIndex].jobMemory()
			}
			// Imported guarded memory may have grown beyond its initial committed Go
			// slice. Re-slice the stable reservation to the current logical size.
			lin, hostErr := dataJM.HostBytesChecked()
			if hostErr != nil {
				initErr = fmt.Errorf("active data segment %d memory %d host access: %w", seg, d.MemoryIndex, hostErr)
				break
			}
			memory64 := c.memoryDef(int(d.MemoryIndex)).Addr64
			off := uint64(d.Offset.Base)
			if d.Offset.HasGlobal {
				if d.Offset.Global < 0 || d.Offset.Global >= len(c.Globals) || d.Offset.Global >= len(globalCells) || globalCells[d.Offset.Global] == nil {
					initErr = fmt.Errorf("data offset global %d out of range", d.Offset.Global)
					break
				}
				off = uint64(uint32(readGlobalObject(globalCells[d.Offset.Global], c.Globals[d.Offset.Global].Type)))
			}
			if len(d.Offset.Expr) != 0 {
				want := ValI32
				if memory64 {
					want = ValI64
				}
				value, err := evalCompiledScalarConstExpr(d.Offset.Expr, want, globalCells, c.Globals, len(importGlobals))
				if err != nil {
					initErr = fmt.Errorf("data offset extended expression: %w", err)
					break
				}
				off = value
			}
			length := uint64(len(d.Bytes))
			if off > ^uint64(0)-length {
				initErr = fmt.Errorf("active data segment %d out of bounds on memory %d: offset %d + length %d overflows u64", seg, d.MemoryIndex, off, len(d.Bytes))
				break
			}
			end := off + length
			if end > uint64(len(lin)) {
				initErr = fmt.Errorf("active data segment %d out of bounds on memory %d: offset %d + length %d > memory size %d", seg, d.MemoryIndex, off, len(d.Bytes), len(lin))
				break
			}
			copy(lin[int(off):int(end)], d.Bytes)
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
	trap := ar.Alloc(runtime.TrapBufferBytes)
	if err := jm.BindTrapCell(trap); err != nil {
		return nil, fmt.Errorf("bind trap cell: %w", err)
	}

	var tableDescPtr uintptr
	if len(tableDesc) != 0 {
		tableDescPtr = uintptr(unsafe.Pointer(&tableDesc[0]))
	}
	jm.CaptureInstanceContextBytes(nativeContext)
	in := &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, syncMode: syncMode, ctrl: ctrl, syncHosts: syncHosts, globals: globals, globalCells: globalCells, tableDescPtr: tableDescPtr, tableDescLen: len(tableDesc), funcRefDescs: funcRefDescs, passiveDataDesc: passiveDataDesc, thunkMem: thunkMem, gc: b.collector,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, c.maxResultSlots), rt: opts.runtime,
		nativeContext: nativeContextPtr,
	}
	b.registeredInstance = in
	if in.syncMode {
		if err := registerHostControl(in); err != nil {
			return nil, fmt.Errorf("instantiate: register host control frame: %w", err)
		}
		defer func() {
			if !b.success {
				unregisterHostControl(in)
			}
		}()
	}
	if memoryCount > 1 {
		in.memoryDir = &instanceMemoryDirectory{memories: memoryObjs, owns: memoryOwns, native: nativeMemoryDir}
	}
	if gcGlobalRootCount != 0 {
		state := in.ensurePluginState()
		state.gcGlobalRoots = gcGlobalRoots
		state.gcGlobalRootCount = gcGlobalRootCount
	}
	if gcArrayElements != nil {
		in.ensurePluginState().gcArrayElements.Store(gcArrayElements)
	}
	if gcRefTestTable != nil {
		in.ensurePluginState().gcRefTestTable.Store(gcRefTestTable)
	}
	if len(nativeTagIDs) != 0 {
		in.ensurePluginState().tagIdentityBase = uintptr(unsafe.Pointer(&nativeTagIDs[0]))
	}
	if opts.origin != InstantiateDirect || opts.pluginGC != nil {
		state := in.ensurePluginState()
		state.origin = opts.origin
		if opts.pluginGC != nil {
			cfg := *opts.pluginGC
			state.gcConfig = &cfg
		}
	}
	if opts.runtime != nil {
		// Once a Runtime-owned Instance exists, every later failure must dispose it
		// through the normal lifecycle before the instantiation error escapes.
		defer func() {
			if recovered := recover(); recovered != nil {
				result = nil
				if panicErr, ok := recovered.(error); ok {
					err = fmt.Errorf("wago: instantiation panicked after instance creation: %w", panicErr)
				} else {
					err = fmt.Errorf("wago: instantiation panicked after instance creation: %v", recovered)
				}
			}
			if b.success {
				return
			}
			b.success = true // the normal Close path now owns all instance resources
			err = joinPrimary(err, in.Close())
		}()
	}
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
		if opts.runtime == nil {
			tableRetained := retainProducerRootsInImportedTables(in)
			globalRetained := retainProducerRootsInImportedGlobals(in)
			if tableRetained || globalRetained {
				b.success = true
				_ = in.Close()
			}
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
			caller := in.beginHostCallScope()
			if err := callImportedStart(fn, caller); err != nil {
				return nil, fmt.Errorf("start function %q: %w", key, err)
			}
		} else {
			if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Entry) {
				return nil, fmt.Errorf("start function index %d out of range", c.StartLocalFunc)
			}
			startEntry := base + uintptr(c.Entry[c.StartLocalFunc])
			var startErr error
			if in.syncMode {
				startErr = in.callNativeSync(startEntry)
			} else {
				startErr = in.callNativeAsync(startEntry, false)
			}
			if startErr != nil {
				// Instantiation writes to imported tables are store side effects. If a
				// local funcref remains installed when start traps, the shared table
				// becomes the failed instance's lifetime owner. The table prunes roots
				// no longer present in any slot, so retention stays bounded by its
				// descriptor capacity rather than by failed-instantiation count.
				if opts.runtime == nil {
					tableRetained := retainProducerRootsInImportedTables(in)
					globalRetained := retainProducerRootsInImportedGlobals(in)
					if tableRetained || globalRetained {
						b.success = true
						_ = in.Close()
					}
				}
				return nil, fmt.Errorf("start function trapped: %w", startErr)
			}
		}
	}

	b.success = true
	return in, nil
}

func callImportedStart(fn HostFunc, caller instanceHostModule) (err error) {
	defer caller.scope.end(caller.generation)
	defer func() {
		if recovered := recover(); recovered != nil {
			switch value := recovered.(type) {
			case HostExit:
				err = &ExitError{Code: value.Code}
			case *HostExit:
				if value == nil {
					err = fmt.Errorf("host start panicked with a nil *HostExit")
				} else {
					err = &ExitError{Code: value.Code}
				}
			case error:
				err = fmt.Errorf("host start panicked: %w", value)
			default:
				err = fmt.Errorf("host start panicked: %v", value)
			}
		}
	}()
	fn(caller, nil, nil)
	return nil
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

func funcSigLocalRegABI(sig FuncSig) bool {
	if len(sig.Results) > 2 {
		return false
	}
	if len(sig.Results) == 2 && ((sig.Results[0] != ValI32 && sig.Results[0] != ValI64) || (sig.Results[1] != ValI32 && sig.Results[1] != ValI64)) {
		return false
	}
	gp, fp := 0, 0
	for _, t := range sig.Params {
		switch t {
		case ValI32, ValI64:
			gp++
		case ValF32, ValF64:
			fp++
		default:
			return false
		}
	}
	if gp > 7 || fp > 8 {
		return false
	}
	for _, t := range sig.Results {
		if t != ValI32 && t != ValI64 && t != ValF32 && t != ValF64 {
			return false
		}
	}
	return true
}

func funcSigReferenceResultRegABI(sig FuncSig) bool {
	if len(sig.Results) != 1 || sig.Results[0] != ValFuncRef {
		return false
	}
	gp, fp := 0, 0
	for _, t := range sig.Params {
		switch t {
		case ValI32, ValI64:
			gp++
		case ValF32, ValF64:
			fp++
		default:
			return false
		}
	}
	return gp <= 7 && fp <= 8
}

func funcSigIntRegABI(sig FuncSig) bool {
	// Up to two integer results ride the register ABI (RAX/RDX on amd64, X0/X1 on
	// arm64); the int-only check below covers both result types.
	if len(sig.Results) > 2 || len(sig.Params) > 8 {
		return false
	}
	for _, t := range append(append([]ValType{}, sig.Params...), sig.Results...) {
		if t != ValI32 && t != ValI64 {
			return false
		}
	}
	return true
}

// buildHostFuncThunks generates wrapper-ABI targets for every host-bound import.
// The same target serves direct imported calls through the instance dispatch
// table and host funcrefs stored in Wasm tables.
func buildHostFuncThunks(c *Compiled, imports Imports, syncMode bool) (map[uint32]uint64, []byte, error) {
	var blob []byte
	offs := map[uint32]int{}
	for fidx := 0; fidx < c.NumImports; fidx++ {
		key := c.Imports[fidx]
		if _, isCross := imports[key].(*InstanceExport); isCross {
			continue // cross-instance funcref, not a host function
		}
		if syncMode {
			if fidx >= len(c.importFuncSigs) {
				return nil, nil, fmt.Errorf("import %q wrapper signature is missing", key)
			}
			sig := c.importFuncSigs[fidx]
			paramSlots, err := valTypesSlots(sig.Params)
			if err != nil {
				return nil, nil, fmt.Errorf("import %q wrapper params: %w", key, err)
			}
			resultSlots, err := valTypesSlots(sig.Results)
			if err != nil {
				return nil, nil, fmt.Errorf("import %q wrapper results: %w", key, err)
			}
			if paramSlots > runtime.MaxHostArity || resultSlots > runtime.MaxHostArity {
				return nil, nil, fmt.Errorf("import %q uses %d param slot(s), %d result slot(s); synchronous host wrappers support at most %d slots in each direction", key, paramSlots, resultSlots, runtime.MaxHostArity)
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
				return nil, nil, fmt.Errorf("import %q is %T; async host wrappers support wago.HostFunc or *wago.HostFuncRef bindings", key, imports[key])
			}
		}
	}
	if len(blob) == 0 {
		return nil, nil, nil
	}
	mem, base, err := runtime.MapCode(blob)
	if err != nil {
		return nil, nil, fmt.Errorf("host import wrapper thunk: %w", err)
	}
	addr := make(map[uint32]uint64, len(offs))
	for fidx, o := range offs {
		addr[fidx] = uint64(base) + uint64(o)
	}
	return addr, mem, nil
}
