package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// InstantiateOptions configures instance creation from a *Compiled.
type InstantiateOptions struct {
	// MaxCompiledMetadataBytes bounds the frozen Go metadata snapshot. Zero
	// uses the source policy or the 256 MiB default; native instance storage
	// has a separate quota. A stricter limit also applies to an existing snapshot.
	MaxCompiledMetadataBytes uint64
	Imports                  Imports
	GC                       GCConfig
	store                    *referenceStore

	ownedImports             bool // private Runtime resolution has transferred ownership
	runtime                  *Runtime
	origin                   InstantiateOrigin
	pluginGC                 *GCConfig
	pluginGCImports          map[uint32]struct{}
	forceSyncHost            bool
	afterCreate              func(*Instance) error
	moduleIdentity           ModuleIdentity
	operationReservation     *pluginOperationReservation
	runtimeReservation       *runtimeInstanceReservation
	independentInstances     bool
	hasExecutionPolicy       bool
	nativeStackBytes         uint64
	memoryLimitPages         uint32
	maxInstanceMetadataBytes uint64
}

// Instantiable is the set of sources Instantiate accepts. The interface is
// sealed so only *Compiled implements it.
type Instantiable interface {
	instantiable()
}

func (*Compiled) instantiable() {}

// Instantiate creates a live instance from a *Compiled, wiring the module's
// imports from opts and running its start function.
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
	collectorShared     bool
	gcDomain            *gcStoreDomain
	gcDomainID          uint64
	gcTypeMap           *gcTypeMapping
	success             bool
	registeredInstance  *Instance
	functionAttachments functionImportAttachments
	hostAttachments     hostFuncRefAttachments
	tableAttachments    tableImportAttachments
	globalAttachments   globalImportAttachments
	tagAttachments      tagImportAttachments
	moduleUse           *Module
}

// instantiateCore maps code and applies explicit instance options.
func instantiateCore(c *Compiled, opts InstantiateOptions) (*Instance, error) {
	return instantiateCoreWithModuleLease(c, opts, nil)
}

func instantiateCoreWithModuleLease(c *Compiled, opts InstantiateOptions, moduleUse *Module) (*Instance, error) {
	defer goruntime.KeepAlive(c)
	// Keep validation, native linking, public lookup, and teardown on one binding set.
	if opts.Imports != nil && !opts.ownedImports {
		imports := make(Imports, len(opts.Imports))
		for key, value := range opts.Imports {
			imports[key] = value
		}
		opts.Imports = imports
	}
	b := instanceBuilder{c: c, opts: opts, imports: opts.Imports, moduleUse: moduleUse}
	defer b.releaseModuleUse()
	if c == nil {
		return nil, errors.New("wago: instantiate: nil compiled module")
	}
	// Closed is an ownership state, not malformed metadata. Check it before
	// preflight/validation so Close remains the authoritative error even after it
	// has released and cleared the native-code view.
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	var err error
	c, err = c.freezeExecution(opts.MaxCompiledMetadataBytes)
	if err != nil {
		return nil, err
	}
	b.c = c
	if err := c.preflightImportBindings(opts.Imports); err != nil {
		return nil, err
	}
	return b.instantiate()
}

func (b *instanceBuilder) releaseModuleUse() {
	if b.moduleUse == nil {
		return
	}
	b.moduleUse.endUse()
	b.moduleUse = nil
}

func (b *instanceBuilder) validateCompiled() error {
	if err := b.c.validateImportBindingsWithPluginGC(b.imports, b.opts.store, b.opts.pluginGCImports); err != nil {
		return err
	}
	return b.c.validateCached()
}

func (c *Compiled) allowsIndependentInstanceExecution(imports Imports, enabled bool) bool {
	if !enabled {
		return false
	}
	if c.memoryImportCount() != 0 || c.tableImportCount() != 0 || len(c.GlobalImports) != 0 {
		return false
	}
	for _, key := range c.Imports {
		if _, ok := imports[key].(*InstanceExport); ok {
			return false
		}
	}

	return true
}

func (c *Compiled) arenaNeedForImports(imports Imports, syncMode bool) int {
	need := c.instantiateArenaNeed
	baselineHostBytes := 0
	if c.needsPublicFuncrefHostReentry() || c.usesGCStructHelpers() || c.usesGCArrayHelpers() || c.usesDynamicFuncRefTest() || c.usesAtomicWaitHelpers() {
		baselineHostBytes = c.hostCtrlFrameBytes()
	} else if len(c.Imports) > 0 {
		baselineHostBytes = runtime.HostCallLogBytes
	}
	actualHostBytes := 0
	if syncMode {
		// Runtime instantiation always installs the synchronous control frame,
		// including for modules without function imports: public funcref calls and
		// nested runtime entry share the same parked-host context.
		actualHostBytes = c.hostCtrlFrameBytes()
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

func enforceMemoryPageQuota(c *Compiled, imports Imports, limit uint32) error {
	if c == nil || limit == 0 {
		return nil
	}
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		pages := def.Min
		if def.ImportKey != "" {
			memory, ok := imports.memory(def.ImportKey)
			if !ok {
				continue // the normal linker reports the missing import.
			}
			currentPages, ok := memory.currentPages()
			if !ok {
				continue // the normal linker reports the closed owner.
			}
			pages = uint64(currentPages)
		}
		if pages > uint64(limit) {
			return &runtime.ResourceLimitError{
				Resource:  fmt.Sprintf("memory %d live pages", i),
				Scope:     "instance",
				Requested: pages,
				Limit:     uint64(limit),
			}
		}
	}
	return nil
}

func (b *instanceBuilder) prepareCollector() error {
	needsExternConversion := b.c.stagedGCStructProduct().requiresExternConversion()
	needsHelpers := b.c.usesGCStructHelpers() || b.c.usesGCArrayHelpers()
	needsRuntimeDomain := b.c.needsRuntimeGCCollectorDomain()
	if !needsRuntimeDomain && !needsHelpers && ((!gc.HasHeapObjectTypes(b.c.GCTypeDescs) && !needsExternConversion) || (!needsExternConversion && (b.c.collectorFreeStructuralMetadata() || b.c.stagedGCTypeSubtypingProduct() != 0 || b.c.collectorFreeGCArrayMetadata()))) {
		return nil
	}
	gcConfig := b.opts.GC
	if b.c.usesGenericGCExecution() && b.c.genericGCFrameRoots() == nil {
		// Generic products outside the validated native root-map slice still do
		// not publish every native reference at allocating helper safepoints. Keep
		// them stable and bounded rather than scanning an incomplete frame.
		gcConfig.Profile = gc.ProfileThroughput
		gcConfig.DisableCollection = true
	}
	// Atomic waits park while holding an instance-local native execution lease.
	// Until their compiler callsites publish exact GC frame roots, keep GC+Threads
	// modules in private collector domains so a same-memory notifier never waits
	// behind the parked invocation's Runtime-wide collector lease.
	if b.opts.store != nil && !b.opts.store.private && needsRuntimeDomain && b.c.sharedGCPersistentDomainSafe() && !b.c.usesAtomicWaitHelpers() {
		preferred, err := preferredGCCollectorFromImports(b.c, b.imports, b.opts.store)
		if err != nil {
			return err
		}
		collector, mapping, err := b.opts.store.acquireGCCollector(gcConfig, b.c, preferred)
		if err != nil {
			return err
		}
		b.collector = collector
		b.collectorShared = true
		b.gcDomain = b.opts.store.gcDomainForCollector(collector)
		if b.gcDomain != nil {
			b.gcDomainID = b.gcDomain.id
		}
		if b.gcDomainID == 0 {
			b.opts.store.releaseUnclaimedGCCollector(collector)
			b.collector = nil
			b.collectorShared = false
			return fmt.Errorf("wago: Runtime GC domain has no native identity")
		}
		b.gcTypeMap = mapping
		return nil
	}
	descs := b.c.GCTypeDescs
	var mapping *gcTypeMapping
	if len(b.c.Types) == len(descs) && hasEquivalentLocalGCHeapTypes(b.c) {
		var err error
		mapping, descs, _, err = gcCanonicalTypePlan(b.c, nil, nil, true)
		if err != nil {
			return err
		}
	}
	if b.opts.store != nil && !b.opts.store.private {
		if err := b.opts.store.bindFuncrefGCStore(); err != nil {
			return err
		}
	}
	collector, err := gc.NewCollector(gcConfig, descs)
	if err != nil {
		return err
	}
	b.collector = collector
	b.gcTypeMap = mapping
	b.gcDomainID = newGCDomainIdentity()
	return nil
}

func (b *instanceBuilder) buildNativeGCInstanceView(localTypes []gc.TypeID) (*gc.NativeInstanceView, error) {
	if b.gcDomain != nil {
		b.gcDomain.invocationMu.Lock()
		defer b.gcDomain.invocationMu.Unlock()
	}
	return gc.BuildNativeInstanceView(b.collector, localTypes)
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
			if err := b.hostAttachments.attach(value, b.opts.store, b.c.importFuncSigs[i], b.collector, b.gcDomainID, b.c, i); err != nil {
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
		if err := b.globalAttachments.attach(global, b.opts.store, b.collector); err != nil {
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
		b.registeredInstance.referenceLifetime().notifyStore(b.registeredInstance.refStore, referenceLifetimeClosed)
	}
	if b.collector != nil {
		if b.collectorShared && b.opts.store != nil && (b.registeredInstance == nil || b.registeredInstance.refStore == nil) {
			b.opts.store.releaseUnclaimedGCCollector(b.collector)
		} else if !b.collectorShared {
			b.collector.Close()
		}
	}
}

func (b *instanceBuilder) instantiate() (result *Instance, err error) {
	if err := b.validateCompiled(); err != nil {
		return nil, err
	}
	c, opts, imports := b.c, b.opts, b.imports
	if !opts.hasExecutionPolicy && c.validateMemo != nil {
		if opts.memoryLimitPages == 0 {
			opts.memoryLimitPages = c.validateMemo.memoryLimitPages
		}
		if opts.maxInstanceMetadataBytes == 0 {
			opts.maxInstanceMetadataBytes = c.validateMemo.maxInstanceMetadataBytes
		}
	}
	syncMode := c.importsRequireSync(imports, opts.forceSyncHost)
	arenaNeed := c.arenaNeedForImports(imports, syncMode)
	if c.memoryCount() == 1 && (opts.memoryLimitPages != 0 || c.memoryImport != "" && c.threadedMemory0()) {
		if arenaNeed > maxInt()-abi.MemoryDirEntryBytes {
			return nil, fmt.Errorf("instance metadata footprint overflows memory policy directory")
		}
		arenaNeed += abi.MemoryDirEntryBytes
	}
	if opts.maxInstanceMetadataBytes != 0 && uint64(arenaNeed) > opts.maxInstanceMetadataBytes {
		return nil, &runtime.ResourceLimitError{
			Resource:  "instance metadata bytes",
			Scope:     "instance",
			Requested: uint64(arenaNeed),
			Limit:     opts.maxInstanceMetadataBytes,
		}
	}
	if err := enforceMemoryPageQuota(c, imports, opts.memoryLimitPages); err != nil {
		return nil, err
	}
	if err := b.prepareCollector(); err != nil {
		return nil, err
	}
	needsExternConversion := c.stagedGCStructProduct().requiresExternConversion()
	var conversionStore *referenceStore
	var gcExternConversion *gcExternConversionState
	if needsExternConversion {
		conversionStore = opts.store
		if conversionStore == nil {
			conversionStore = newReferenceStore(true)
		}
		gcExternConversion, err = newGCExternConversionState(conversionStore, b.collector)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		if !b.success && gcExternConversion != nil {
			_ = gcExternConversion.close()
		}
	}()
	defer func() {
		if !b.success {
			b.rollbackPreparedState()
		}
	}()
	importGlobals, err := b.attachImports()
	if err != nil {
		return nil, err
	}
	stackBytes := opts.nativeStackBytes
	if stackBytes == 0 {
		stackBytes = c.nativeStackBytes()
	}
	if stackBytes == 0 {
		stackBytes = runtime.DefaultNativeStackBytes
	}
	eng, err := runtime.AcquireEngineWithStackBytes(stackBytes)
	if err != nil {
		return nil, err
	}
	// Memory: a host-imported *Memory if the module imports one, otherwise an
	// instance-owned mapping (guard-page-backed for signals-based modules, so the
	// fault handler catches OOB accesses through the normal Invoke path).
	var (
		jm              *runtime.JobMemory
		memObj          *Memory
		ownsMem         bool
		threadedControl bool
		memoryObjs      []*Memory
		memoryOwns      []bool
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
			if err := m.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64, def.Shared); err != nil {
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("imported memory %q limits: %w", c.memoryImport, err)
			}
		}
		// A signals-based module may elide memory-0 memory32 bounds checks and rely
		// on the guard-page fault, so its primary imported memory must be
		// guard-page backed. Host
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
		memObj = m
		if c.threadedMemory0() {
			jm, err = runtime.AcquireJobMemoryGrowable(0, 0)
			if err != nil {
				m.detachImporter()
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("allocate threaded instance control: %w", err)
			}
			threadedControl = true
		} else {
			jm = m.jobMemory()
		}
	} else {
		initialBytes, maxBytes := c.memorySizeBytes()
		if c.boundsMode == BoundsChecksSignalsBased || c.prefersGuardMemory() {
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
	needsMemoryDir := memoryCount > 1 || threadedControl || memoryCount != 0 && opts.memoryLimitPages != 0
	if needsMemoryDir {
		memoryObjs = make([]*Memory, memoryCount)
		memoryOwns = make([]bool, memoryCount)
		memoryObjs[0], memoryOwns[0] = memObj, ownsMem
	}
	// Release every owned mapping once and detach every distinct imported memory
	// once. Multiple import declarations may deliberately alias one host Memory.
	closeMem := func() {
		if memoryCount <= 1 {
			if threadedControl {
				runtime.ReleaseJobMemory(jm)
			}
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
			if err := memory.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64, def.Shared); err != nil {
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
		maxPages := uint64(65536)
		if def.HasMax {
			maxPages = def.Max
		}
		initialPages := def.Min
		var secondaryJM *runtime.JobMemory
		var allocErr error
		if c.boundsMode == BoundsChecksSignalsBased || c.prefersGuardMemory() {
			// Every owned memory requested by a signals-based configuration uses the guarded
			// representation, even though indexed-memory accesses retain explicit
			// checks. An exported nonzero memory may become memory 0 of another
			// signals-based instance, where guard-backed ownership is mandatory.
			secondaryJM, allocErr = newGuardedJobMemory(int(initialPages)*65536, int(maxPages)*65536)
		} else {
			secondaryJM, allocErr = runtime.AcquireJobMemoryGrowable(int(initialPages)*65536, int(maxPages)*65536)
		}
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
	ar, err := runtime.AcquireArena(arenaNeed)
	if err != nil {
		closeMem()
		runtime.ReleaseEngine(eng)
		return nil, err
	}
	nativeContext := ar.AllocNoZero(runtime.InstanceContextBytes)
	nativeContextPtr := uintptr(unsafe.Pointer(&nativeContext[0]))
	if needsMemoryDir {
		nativeMemoryDir = ar.Alloc(memoryCount * abi.MemoryDirEntryBytes)
		for i, memory := range memoryObjs {
			memoryJM := memory.jobMemory()
			if memoryJM == nil {
				runtime.ReleaseArena(ar)
				closeMem()
				runtime.ReleaseEngine(eng)
				return nil, fmt.Errorf("memory %d owner closed during instantiation", i)
			}
			entry := nativeMemoryDir[i*abi.MemoryDirEntryBytes:]
			binary.LittleEndian.PutUint64(entry[abi.MemoryDirBaseOffset:], uint64(memoryJM.LinMemBase()))
			binary.LittleEndian.PutUint64(entry[abi.MemoryDirCurrentBytesOffset:], uint64(len(memoryJM.HostBytes())))
			binary.LittleEndian.PutUint32(entry[abi.MemoryDirCurrentPagesOffset:], memoryJM.CurrentPages())
			binary.LittleEndian.PutUint32(entry[abi.MemoryDirPolicyMaxPagesOffset:], opts.memoryLimitPages)
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
	// The builder now owns a compiled-code reference. Release the Module lease
	// before lifecycle and start callbacks; Module.Close may close the Compiled
	// while this reference keeps its native mapping live.
	b.releaseModuleUse()
	var hostLog, ctrl []byte
	var syncHosts []syncHostBinding
	if syncMode {
		// Synchronous host-call path: install the control frame (not the async
		// log) as the import ctx. Modules that accept public funcrefs and can call
		// them indirectly also need this frame so an owned host descriptor remains
		// callable after crossing from another instance.
		ctrl = ar.AllocNoZero(c.hostCtrlFrameBytes())
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
		if c.usesDynamicFuncRefTest() {
			typeIDBytes := (4*len(c.FuncTypeID) + 7) &^ 7
			funcRefTypeIDs := ar.Alloc(typeIDBytes)
			binary.LittleEndian.PutUint64(funcRefDescs[runtime.TableEntryCodePtrOffset:], uint64(uintptr(unsafe.Pointer(&funcRefTypeIDs[0]))))
			for fidx := range c.FuncTypeID {
				typeID := ^uint32(0)
				// Imported descriptors are local proxies. Their declared consumer type
				// can be a strict supertype of the attached function's actual provider
				// type, so force the cold attachment-aware classifier for proxy values.
				if local := fidx - c.NumImports; local >= 0 && local < len(c.Funcs) && c.Funcs[local].HasTypeIndex {
					typeID = c.Funcs[local].TypeIndex
				}
				binary.LittleEndian.PutUint32(funcRefTypeIDs[4*fidx:], typeID)
			}
		}
		mayCarryFuncref := func(t ValType) bool { return t == ValFuncRef || t == ValAnyRef }
		localFuncrefsMayEscape := c.tableImport != ""
		if !localFuncrefsMayEscape {
			for i := range c.extraTables {
				if c.extraTables[i].ImportKey != "" {
					localFuncrefsMayEscape = true
					break
				}
			}
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			localFuncrefsMayEscape = len(c.tableExports) != 0
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			for i := range c.memoryDir.ehTags {
				typeIdx := c.memoryDir.ehTags[i].TypeIndex
				if uint64(typeIdx) >= uint64(len(c.Types)) || c.Types[typeIdx].Kind != CompositeTypeFunction {
					continue
				}
				for _, param := range c.Types[typeIdx].Params {
					if abiType, ok := param.ABIType(c.Types); ok && mayCarryFuncref(abiType) {
						localFuncrefsMayEscape = true
						break
					}
				}
				if localFuncrefsMayEscape {
					break
				}
			}
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			localFuncrefsMayEscape = c.dynamicFuncrefEscape
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			for i := range c.importFuncSigs {
				for _, param := range c.importFuncSigs[i].Params {
					if mayCarryFuncref(param) {
						localFuncrefsMayEscape = true
						break
					}
				}
				for _, result := range c.importFuncSigs[i].Results {
					if result == ValAnyRef {
						localFuncrefsMayEscape = true
						break
					}
				}
				if localFuncrefsMayEscape {
					break
				}
			}
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			for i := range c.GlobalImports {
				if mayCarryFuncref(c.GlobalImports[i].Type) {
					localFuncrefsMayEscape = true
					break
				}
			}
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			for _, globalIdx := range c.GlobalExports {
				if globalIdx >= 0 && globalIdx < len(c.Globals) && mayCarryFuncref(c.Globals[globalIdx].Type) {
					localFuncrefsMayEscape = true
					break
				}
			}
		}
		if goruntime.GOARCH == "arm64" && !localFuncrefsMayEscape {
			for _, fidx := range c.Exports {
				local := fidx - c.NumImports
				if local < 0 || local >= len(c.Funcs) {
					continue
				}
				for _, param := range c.Funcs[local].Params {
					if param == ValAnyRef {
						localFuncrefsMayEscape = true
						break
					}
				}
				for _, result := range c.Funcs[local].Results {
					if mayCarryFuncref(result) {
						localFuncrefsMayEscape = true
						break
					}
				}
				if localFuncrefsMayEscape {
					break
				}
			}
		}
		for fidx := 0; fidx < len(c.FuncTypeID); fidx++ {
			off := (fidx + 1) * runtime.FuncRefDescBytes
			var code, home uint64
			targetContext := uint64(nativeContextPtr)
			kind := abi.FuncRefEntryInvalid
			if li := fidx - c.NumImports; li >= 0 && li < len(c.Entry) {
				internal := c.Entry[li]
				if li < len(c.InternalEntry) {
					internal = internalEntryOffset(c.InternalEntry[li])
				}
				regABIEnabled := !c.registerABIDisabled
				stagedTailRegABI := regABIEnabled && c.stagedFeatures().IsEnabled(CoreFeatureTailCall) && (funcSigLocalRegABI(c.Funcs[li]) || funcSigReferenceResultRegABI(c.Funcs[li]))
				localRegABI := regABIEnabled && funcSigLocalRegABI(c.Funcs[li])
				// Equal wrapper/internal offsets on a register-ABI function encode an
				// intentionally wrapperless direct-only function. It cannot be a valid
				// ref.func target; leave its unused descriptor entry invalid instead of
				// publishing the internal ABI under a wrapper tag.
				if internal == c.Entry[li] && (localRegABI || stagedTailRegABI) {
					continue
				}
				code, home = uint64(base)+uint64(c.Entry[li]), selfLinMem
				kind = abi.FuncRefEntryLocalWrapper
				if goruntime.GOARCH == "arm64" && localFuncrefsMayEscape {
					kind = abi.FuncRefEntryCrossInstanceWrapper
				}
				if !localFuncrefsMayEscape && internal != c.Entry[li] && (regABIEnabled && funcSigIntRegABI(c.Funcs[li]) || stagedTailRegABI) {
					code = uint64(base) + uint64(internal)
					kind = abi.FuncRefEntryInternal
				}
			} else if fidx < c.NumImports {
				if ex, ok := imports[c.Imports[fidx]].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.localIdx < len(ex.inst.c.Entry) {
					code = uint64(ex.inst.base) + uint64(ex.inst.c.Entry[ex.localIdx])
					home = uint64(ex.inst.jm.LinMemBase())
					targetContext = uint64(ex.inst.nativeContext)
					kind = abi.FuncRefEntryCrossInstanceWrapper
				} else if addr, ok := thunkAddr[uint32(fidx)]; ok {
					code, home = addr, selfLinMem
					kind = abi.FuncRefEntryHostThunk
				}
			}
			if kind != abi.FuncRefEntryInvalid {
				if code == 0 {
					return nil, fmt.Errorf("instantiate: function %d has a zero %v entry", fidx, kind)
				}
				taggedHome, ok := abi.TagFuncRefHome(home, kind)
				if !ok {
					return nil, fmt.Errorf("instantiate: function %d home pointer collides with descriptor entry tags", fidx)
				}
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryCodePtrOffset:], code)
				binary.LittleEndian.PutUint64(funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], taggedHome)
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
	} else if c.needsFuncRefContextHeader {
		funcRefDescs = ar.Alloc(runtime.FuncRefDescBytes)
		binary.LittleEndian.PutUint64(funcRefDescs[runtime.FuncRefContextOffset:], uint64(nativeContextPtr))
		jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&funcRefDescs[0])))
	}
	var globalCells []*Global
	var instantiationRoots gc.Slots
	writeElemEntry := func(entry []byte, refType ValType, value RefInit) (err error) {
		rootCompactEntry := normalizedElemRefType(refType) == ValAnyRef || normalizedElemRefType(refType) == ValI31Ref
		defer func() {
			if err == nil && rootCompactEntry && len(entry) >= 4 {
				instantiationRoots = append(instantiationRoots, compactRefRootSlot(entry[:4]))
			}
		}()
		if len(value.Expr) != 0 {
			if normalizedElemRefType(refType) != ValAnyRef && normalizedElemRefType(refType) != ValI31Ref && !(normalizedElemRefType(refType) == ValExternRef && needsExternConversion) {
				return fmt.Errorf("GC element expression has incompatible destination %s", refType)
			}
			if len(entry) < 8 {
				return fmt.Errorf("GC element expression entry is truncated")
			}
			bits, err := evalCompiledGCConstExpr(value.Expr, b.collector, b.gcTypeMap, gcExternConversion, c, globalCells, len(globalCells), funcRefDescs, instantiationRoots)
			if err != nil {
				return err
			}
			binary.LittleEndian.PutUint64(entry, bits)
			return nil
		}
		if value.HasGlobal {
			if int(value.GlobalIndex) >= len(globalCells) || globalCells[value.GlobalIndex] == nil {
				return fmt.Errorf("element global %d is unavailable", value.GlobalIndex)
			}
			if value.I31Wrap {
				if normalizedElemRefType(refType) != ValI31Ref || len(entry) < 8 {
					return fmt.Errorf("element global %d i31 wrapper has incompatible destination %s", value.GlobalIndex, refType)
				}
				bits := uint32(readGlobalObject(globalCells[value.GlobalIndex], ValI32))
				binary.LittleEndian.PutUint64(entry, uint64(bits<<1|1))
				return nil
			}
			bits := readGlobalObject(globalCells[value.GlobalIndex], normalizedElemRefType(refType))
			switch normalizedElemRefType(refType) {
			case ValFuncRef:
				if bits == 0 {
					clear(entry)
					return nil
				}
				if len(entry) < runtime.TableEntryBytes {
					return fmt.Errorf("funcref element global descriptor is truncated")
				}
				descriptor := unsafe.Slice((*byte)(offHeapPtr(uintptr(bits))), runtime.FuncRefDescBytes)
				// Keep this explicit: Go 1.26.5 can ICE while lowering copy from
				// this unsafe slice during Darwin/arm64 to AMD64 cross-builds.
				for index := 0; index < runtime.TableEntryBytes; index++ {
					entry[index] = descriptor[index]
				}
				// A canonical descriptor may select its producer's internal register-ABI
				// entry. Once copied through an imported global into another instance's
				// table, use the producer's offset-0 wrapper and explicit cross-instance
				// home tag so ordinary call_indirect performs the required context switch.
				global := globalCells[value.GlobalIndex]
				if global.owner != nil && global.owner.instance != nil && global.owner.instance.nativeContext != nativeContextPtr {
					producer := global.owner.instance
					if fidx, ok := producer.funcrefDescriptorIndex(bits); ok {
						li := fidx - producer.c.NumImports
						if li >= 0 && li < len(producer.c.Entry) {
							taggedHome, ok := abi.TagFuncRefHome(uint64(producer.jm.LinMemBase()), abi.FuncRefEntryCrossInstanceWrapper)
							if !ok {
								return fmt.Errorf("funcref element global producer home collides with descriptor tags")
							}
							binary.LittleEndian.PutUint64(entry[runtime.TableEntryCodePtrOffset:], uint64(producer.base)+uint64(producer.c.Entry[li]))
							binary.LittleEndian.PutUint64(entry[runtime.TableEntryHomeLinMemOffset:], taggedHome)
						}
					}
				}
				return nil
			case ValExternRef, ValAnyRef, ValI31Ref:
				if len(entry) < 8 {
					return fmt.Errorf("reference element global entry is truncated")
				}
				binary.LittleEndian.PutUint64(entry, bits)
				return nil
			case ValExnRef:
				if bits != 0 {
					return fmt.Errorf("non-null exception element globals require active handler ownership")
				}
				clear(entry)
				return nil
			default:
				return fmt.Errorf("unsupported element global reference type %s", refType)
			}
		}
		switch normalizedElemRefType(refType) {
		case ValExternRef, ValExnRef, ValAnyRef:
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
			if value.Null {
				clear(entry)
				return nil
			}
			if writeTableEntry == nil {
				return fmt.Errorf("non-null funcref element has no descriptor arena")
			}
			writeTableEntry(entry, value.FuncIndex)
			return nil
		default:
			return fmt.Errorf("unsupported element reference type %s", refType)
		}
	}

	var globals []byte
	var gcGlobalRoots []gcGlobalRootMapping
	var gcGlobalRootCount uint32
	var genericGCGlobalRoots []gcGlobalRootMapping
	runtimeGCExecution := c.needsRuntimeGCCollectorDomain()
	globalCells = make([]*Global, len(c.Globals))
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
					ref, slot, err := instantiateGCStructGlobal(b.collector, b.gcTypeMap, c.GCTypeDescs, gcInit)
					if err != nil {
						return nil, fmt.Errorf("global %d GC struct initializer: %w", i, err)
					}
					bits = uint64(ref)
					mapping := gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot}
					if runtimeGCExecution {
						genericGCGlobalRoots = append(genericGCGlobalRoots, mapping)
					} else {
						gcGlobalRoots = append(gcGlobalRoots, mapping)
						gcGlobalRootCount++
					}
				} else if gcInit, ok := c.gcArrayGlobalInit(i); ok {
					ref, slot, err := instantiateGCArrayGlobal(b.collector, b.gcTypeMap, c.GCTypeDescs, gcInit, funcRefDescs)
					if err != nil {
						return nil, fmt.Errorf("global %d GC array initializer: %w", i, err)
					}
					bits = uint64(ref)
					mapping := gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot}
					if runtimeGCExecution {
						genericGCGlobalRoots = append(genericGCGlobalRoots, mapping)
					} else {
						gcGlobalRoots = append(gcGlobalRoots, mapping)
						gcGlobalRootCount++
					}
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
					var value uint64
					var err error
					if g.Type == ValAnyRef || g.Type == ValI31Ref || (g.Type == ValExternRef && needsExternConversion) {
						value, err = evalCompiledGCConstExpr(g.InitExpr, b.collector, b.gcTypeMap, gcExternConversion, c, globalCells, i, funcRefDescs, instantiationRoots)
					} else {
						value, err = evalCompiledScalarConstExpr(g.InitExpr, g.Type, globalCells, c.Globals, constExprGlobalScope{context: constExprGlobalInitializer, limit: i})
					}
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
			if b.collector != nil && isGCRefValType(g.Type) && len(cell.cell) >= 4 {
				// Keep the actual native cell mutable during the remainder of
				// instantiation, before the completed Instance publishes its permanent
				// global/table root views.
				instantiationRoots = append(instantiationRoots, compactRefRootSlot(cell.cell[:4]))
			}
			if b.collector != nil && runtimeGCExecution && i >= len(importGlobals) && isGCRefValType(g.Type) {
				mapped := false
				for _, mapping := range genericGCGlobalRoots {
					if mapping.GlobalIndex == uint32(i) {
						mapped = true
						break
					}
				}
				if !mapped {
					ref := gc.Ref(uint32(readGlobalObject(cell, g.Type)))
					slot, err := b.collector.NewCheckedGlobalSlot(ref)
					if err != nil {
						return nil, fmt.Errorf("global %d generic GC root: %w", i, err)
					}
					genericGCGlobalRoots = append(genericGCGlobalRoots, gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot})
				}
			}
		}
		if len(c.Entry) > 0 {
			jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
		}
	}

	var gcRefTestTable *gcRefTestTableState
	var gcRefTestDescriptors [maxGCRefTestTables][]byte
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
				if err := b.tableAttachments.attach(t, c.tableElementType(tableIndex), exact, c.Types, opts.store, b.collector, def.Addr64); err != nil {
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
				if uint64(size) < importDef.Min {
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
					if uint64(capacity) > importDef.Max {
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
			if product := c.stagedGCStructProduct(); product.requiresRefTableState() && tableIndex < len(gcRefTestDescriptors) {
				gcRefTestDescriptors[tableIndex] = desc
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
				value, err := evalCompiledScalarConstExpr(el.Offset.Expr, offsetType, globalCells, c.Globals, constExprGlobalScope{context: constExprElementOffset, limit: len(c.Globals)})
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
		if product := c.stagedGCStructProduct(); initErr == nil && product.requiresRefTableState() {
			tableCount := c.tableCount()
			valid := tableCount == 1 && c.tableEntryBytes(0) == 8
			if product == stagedGCStructRefTestAbstract {
				valid = tableCount == 3 && c.tableEntryBytes(0) == 8 && c.tableEntryBytes(1) == runtime.TableEntryBytes && c.tableEntryBytes(2) == 8
			}
			if !valid {
				initErr = fmt.Errorf("GC ref.test product has an invalid mixed-table layout")
			} else {
				canonicalTypes, err := b.gcTypeMap.canonicalTypes(product.refTestCanonicalTypes())
				if err != nil {
					initErr = err
				} else {
					gcRefTestTable, initErr = newGCRefTestTableState(b.collector, gcRefTestDescriptors[:tableCount], 0, canonicalTypes)
				}
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
				gcArrayElements, initErr = instantiateGCArrayElementSegment(b.collector, b.gcTypeMap, c.GCTypeDescs, c.memoryDir.gcArrayElement, desc)
			}
		}
		jm.SetPassiveElemPtr(uintptr(unsafe.Pointer(&edesc[0])))
	}

	var passiveDataDesc []byte
	if len(c.PassiveData) > 0 {
		// Descriptor layout is shared with the JIT: {ptr u64, len u32, pad u32}.
		// Descriptors are per-instance because data.drop mutates len. Passive bytes
		// are retained by c; active slots have nil bytes and start at length zero.
		desc := ar.Alloc(runtime.PassiveDataDescBytes * len(c.PassiveData))
		for i, d := range c.PassiveData {
			off := i * runtime.PassiveDataDescBytes
			if len(d.Bytes) != 0 {
				binary.LittleEndian.PutUint64(desc[off:], uint64(uintptr(unsafe.Pointer(&d.Bytes[0]))))
			}
			binary.LittleEndian.PutUint32(desc[off+8:], uint32(len(d.Bytes)))
		}
		jm.SetPassiveDataPtr(uintptr(unsafe.Pointer(&desc[0])))
		passiveDataDesc = desc
	}

	if initErr == nil && len(c.Data) > 0 {
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
				value, err := evalCompiledScalarConstExpr(d.Offset.Expr, want, globalCells, c.Globals, constExprGlobalScope{context: constExprDataOffset, limit: len(c.Globals)})
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
	var gcNativeTypes []gc.TypeID
	var gcNativeView *gc.NativeInstanceView
	if b.collector != nil && c.needsNativeGCABI() {
		gcNativeTypes = make([]gc.TypeID, len(c.Types))
		for local := range gcNativeTypes {
			domain, ok := b.gcTypeMap.domain(uint32(local))
			if !ok {
				return nil, fmt.Errorf("instantiate: native GC type %d has no canonical domain mapping", local)
			}
			gcNativeTypes[local] = domain
		}
		gcNativeView, err = b.buildNativeGCInstanceView(gcNativeTypes)
		if err != nil {
			return nil, fmt.Errorf("instantiate: native GC metadata view: %w", err)
		}
		jm.SetGCNativeViewPtr(uintptr(unsafe.Pointer(gcNativeView)))
	}
	jm.CaptureInstanceContextBytes(nativeContext)
	binary.LittleEndian.PutUint64(nativeContext[runtime.InstanceContextGCDomainOffset:], b.gcDomainID)
	if gcNativeView != nil {
		binary.LittleEndian.PutUint64(nativeContext[runtime.InstanceContextGCNativeViewOffset:], uint64(uintptr(unsafe.Pointer(gcNativeView))))
	}
	in := &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, hosts: imports.hostFuncs(), imports: imports, hostLog: hostLog, syncMode: syncMode, ctrl: ctrl, syncHosts: syncHosts, globals: globals, globalCells: globalCells, tableDescPtr: tableDescPtr, tableDescLen: len(tableDesc), funcRefDescs: funcRefDescs, passiveDataDesc: passiveDataDesc, thunkMem: thunkMem, gc: b.collector, gcTypeMap: b.gcTypeMap, gcNativeView: gcNativeView,
		serArgs: serArgs, results: results, trap: trap, resultVals: make([]uint64, c.maxResultSlots), rt: opts.runtime,
		nativeContext:   nativeContextPtr,
		moduleIdentity:  opts.moduleIdentity,
		pluginGCImports: opts.pluginGCImports,
	}
	independentInstances := c.independentInstances
	if opts.hasExecutionPolicy {
		independentInstances = opts.independentInstances
	}
	// Instances in one Runtime GC domain share a mutable collector. Keep their
	// complete native activations under the process-wide execution lease: helper
	// calls take the domain lock, but native allocation fast paths also mutate the
	// collector and cannot safely overlap another tenant. Private collectors may
	// retain instance-local execution.
	if !b.collectorShared && c.allowsIndependentInstanceExecution(imports, independentInstances) {
		in.executionFlags.Store(executionFlagIndependent)
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
	if memoryCount > 1 || threadedControl {
		in.memoryDir = &instanceMemoryDirectory{memories: memoryObjs, owns: memoryOwns, native: nativeMemoryDir}
	}
	if gcGlobalRootCount != 0 {
		state := in.ensurePluginState()
		state.gcGlobalRoots = gcGlobalRoots
		state.gcGlobalRootCount = gcGlobalRootCount
	}
	if b.collector != nil && runtimeGCExecution {
		public := in.publicGCState()
		public.globalRoots = genericGCGlobalRoots
		for i, g := range c.Globals {
			if !isGCRefValType(g.Type) || i >= len(globalCells) || globalCells[i] == nil {
				continue
			}
			mapped := false
			for _, mapping := range public.globalRoots {
				if mapping.GlobalIndex == uint32(i) {
					mapped = true
					break
				}
			}
			if mapped {
				continue
			}
			bits := readGlobalObject(globalCells[i], g.Type)
			ref := gc.Ref(uint32(bits))
			if bits != uint64(ref) {
				return nil, fmt.Errorf("global %d contains non-compact generic GC reference %#x", i, bits)
			}
			slot, err := b.collector.NewCheckedGlobalSlot(ref)
			if err != nil {
				return nil, fmt.Errorf("global %d generic GC root: %w", i, err)
			}
			public.globalRoots = append(public.globalRoots, gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot})
		}
	}
	if gcArrayElements != nil {
		in.ensurePluginState().gcArrayElements.Store(gcArrayElements)
	}
	if gcRefTestTable == nil && needsExternConversion {
		// Conversion-only modules need the same bounded identity owner without
		// manufacturing a guest table. Table-bearing products populate descriptors
		// above; the zero-table state owns only the conversion bridge.
		gcRefTestTable = &gcRefTestTableState{}
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
		in.beginConstruction(opts.operationReservation)
		if err := opts.runtime.registerInstance(in, opts.runtimeReservation); err != nil {
			in.endConstruction()
			return nil, err
		}
		// Once a Runtime-owned Instance exists, every later failure must dispose it
		// through the normal lifecycle before the instantiation error escapes. The
		// construction lifetime remains active through rollback and is released only
		// after all terminal instantiation observers return.
		defer func() {
			if recovered := recover(); recovered != nil {
				result = nil
				if panicErr, ok := recovered.(error); ok {
					err = fmt.Errorf("wago: instantiation panicked after instance creation: %w", panicErr)
				} else {
					err = fmt.Errorf("wago: instantiation panicked after instance creation: %v", recovered)
				}
			}
			if !b.success {
				b.success = true // the normal Close path now owns all instance resources
				err = joinPrimary(err, in.Close())
			}
			in.endConstruction()
		}()
	}
	if opts.store != nil {
		if err := opts.store.registerInstance(in); err != nil {
			return nil, err
		}
		in.refStore = opts.store
	} else if conversionStore != nil {
		if err := conversionStore.registerInstance(in); err != nil {
			return nil, err
		}
		in.refStore = conversionStore
	}
	if needsExternConversion {
		if in.refStore == nil || gcExternConversion == nil || gcRefTestTable == nil {
			return nil, fmt.Errorf("GC extern conversion ownership is unavailable")
		}
		if err := gcRefTestTable.attachConversion(gcExternConversion); err != nil {
			return nil, err
		}
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
	if opts.afterCreate != nil {
		if err := opts.afterCreate(in); err != nil {
			return nil, err
		}
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
			caller := in.beginHostCallScopeReserved(in.constructionReservationSnapshot())
			if err := callImportedStart(fn, caller); err != nil {
				return nil, fmt.Errorf("start function %q: %w", key, err)
			}
		} else {
			if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Entry) {
				return nil, fmt.Errorf("start function index %d out of range", c.StartLocalFunc)
			}
			startEntry := base + uintptr(c.Entry[c.StartLocalFunc])
			// A Runtime-owned local start executes after the instance joins its shared
			// collector domain. Publish a normal invocation identity and hold the
			// complete-call lease so start-time allocation cannot collect another
			// tenant's native result before public tokenization. The instance gate is
			// acquired first, matching Invoke and prepared-call lock ordering.
			startErr := func() error {
				state := in.ensurePluginState()
				state.invokeMu.Lock()
				id := newInvocationID()
				state.invocationID = id
				defer func() {
					state.invocationID = 0
					state.invokeMu.Unlock()
				}()
				gcLease := in.lockGCInvocation(id)
				defer gcLease.unlock()
				previousReservation := in.swapInvocationReservation(in.constructionReservationSnapshot())
				defer in.swapInvocationReservation(previousReservation)
				if in.syncMode {
					return in.callNativeSync(startEntry)
				}
				return in.callNativeAsync(startEntry, false)
			}()
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
	defer caller.scope.end(caller.generation, caller.parentGeneration)
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
			dispatch := uint32(fidx)
			owned := false
			if owner, ok := imports[key].(*HostFuncRef); ok && owner != nil {
				owner.mu.Lock()
				dispatchIndex := owner.dispatchIndex
				owner.mu.Unlock()
				if binding, ok := owner.dispatchBinding(c, fidx); ok {
					dispatchIndex = binding.dispatchIndex
				}
				dispatch = hostFuncRefDispatchBit | dispatchIndex
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
