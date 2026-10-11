package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// InstantiateOptions configures instance creation from a *Compiled.
type InstantiateOptions struct {
	railshotTailPolicy railshotInstantiateTailPolicy
	// MaxCompiledMetadataBytes bounds the frozen Go metadata snapshot. Zero
	// uses the source policy or the 256 MiB default; native instance storage
	// has a separate quota. A stricter limit also applies to an existing snapshot.
	MaxCompiledMetadataBytes uint64
	// InvokeCacheSlots is the number of export-resolution cache slots per
	// instance. Zero uses four; valid explicit values are 1 through 255.
	InvokeCacheSlots int
	Imports          *Imports
	GC               GCConfig
	store            *referenceStore
	startContext     context.Context

	ownedImports             bool // private Runtime resolution has transferred ownership
	resolvedImports          resolvedImports
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
		case *Imports:
			return InstantiateOptions{Imports: v}, nil
		default:
			return InstantiateOptions{}, fmt.Errorf("wago: Instantiate options must be InstantiateOptions, *Imports, or nil, got %T", args[0])
		}
	default:
		return InstantiateOptions{}, fmt.Errorf("wago: Instantiate expects at most one options argument, got %d", len(args))
	}
}

// instanceBuilder owns the pre-commit state of one instantiation. Concrete
// fields keep unsafe/off-heap ownership visible; no generic cleanup stack is
// used on this allocation-sensitive path.
type instanceBuilder struct {
	//lint:ignore U1000 fields are used only by wago_profile builds; the ordinary placeholder is empty
	profileBuilderState
	c       *Compiled
	opts    InstantiateOptions
	imports resolvedImports

	collector                  *gc.Collector
	constructionGCDomainLocked bool
	collectorShared            bool
	gcDomain                   *gcStoreDomain
	gcDomainID                 uint64
	gcTypeMap                  *gcTypeMapping
	success                    bool
	registeredInstance         *Instance
	functionAttachments        functionImportAttachments
	hostAttachments            hostFuncRefAttachments
	tableAttachments           tableImportAttachments
	globalAttachments          globalImportAttachments
	tagAttachments             tagImportAttachments
	moduleUse                  *Module
}

// instantiateCore maps code and applies explicit instance options.
func instantiateCore(c *Compiled, opts InstantiateOptions) (*Instance, error) {
	return instantiateCoreWithModuleLease(c, opts, nil)
}

func instantiateCoreWithModuleLease(c *Compiled, opts InstantiateOptions, moduleUse *Module) (result *Instance, resultErr error) {
	defer goruntime.KeepAlive(c)
	// Keep validation, native linking, public lookup, and teardown on one binding set.
	imports := opts.resolvedImports
	if !opts.ownedImports && opts.Imports != nil {
		var err error
		imports, err = opts.Imports.snapshot()
		if err != nil {
			return nil, wrapContextError("wago: finalize imports", err)
		}
	}
	b := instanceBuilder{c: c, opts: opts, imports: imports, moduleUse: moduleUse}
	if codeProfileEnabled {
		span := b.beginProfileInstantiation()
		defer finishProfileBoundary(span, &resultErr)
	}
	defer b.releaseModuleUse()
	if opts.InvokeCacheSlots < 0 || opts.InvokeCacheSlots > 255 {
		return nil, fmt.Errorf("wago: InvokeCacheSlots must be between 1 and 255 (or zero for default), got %d", opts.InvokeCacheSlots)
	}
	if opts.startContext != nil {
		if err := opts.startContext.Err(); err != nil {
			return nil, err
		}
	}
	if c == nil {
		return nil, errors.New("wago: instantiate: nil compiled module")
	}
	// Closed is an ownership state, not malformed metadata. Check it before
	// preflight/validation so Close remains the authoritative error even after it
	// has released and cleared the native-code view.
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if err := railshotValidateReferenceFunctionInstance(c, opts); err != nil {
		return nil, err
	}
	if err := railshotValidateResolvedImports(c, imports, opts.store); err != nil {
		return nil, err
	}
	if opts.forceSyncHost {
		if _, err := runtime.HostCtrlFrameBytesForSlots(runtime.MaxHostArity); err != nil {
			return nil, wrapContextError("instantiate: synchronous host protocol", err)
		}
	}
	mappingOwner := c
	if moduleUse != nil && moduleUse.compiledView != nil {
		mappingOwner = moduleUse.compiledView
	}
	if err := mappingOwner.prepareCodeMapping(); err != nil {
		return nil, wrapContextError("wago: instantiate: map code", err)
	}
	var err error
	c, err = c.freezeExecution(opts.MaxCompiledMetadataBytes)
	if err != nil {
		return nil, err
	}
	b.c = c
	if err := c.preflightImportBindings(imports); err != nil {
		return nil, err
	}
	return b.instantiate(mappingOwner)
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

func (c *Compiled) allowsIndependentInstanceExecution(imports resolvedImports, enabled bool) bool {
	if !enabled {
		return false
	}
	if c.memoryImportCount() != 0 || c.tableImportCount() != 0 || len(c.GlobalImports) != 0 {
		return false
	}
	for i := range c.Imports {
		if _, ok := imports[c.functionImportBindingKey(i)].(*InstanceExport); ok {
			return false
		}
	}

	return true
}

func (c *Compiled) arenaNeedForImports(imports resolvedImports, syncMode bool) int {
	need := c.instantiateArenaNeed + railshotNativeInvocationContextBytes - runtime.InstanceContextBytes
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
		for i := range c.Imports {
			if _, cross := imports[c.functionImportBindingKey(i)].(*InstanceExport); !cross {
				actualHostBytes = runtime.HostCallLogBytes
				break
			}
		}
	}
	return need - baselineHostBytes + actualHostBytes + railshotNativeImportArenaBytes(c, imports) + railshotNativeTailArenaBytes(c)
}

func enforceMemoryPageQuota(c *Compiled, imports resolvedImports, limit uint32) error {
	if c == nil || limit == 0 {
		return nil
	}
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		pages := def.Min
		if def.ImportKey != "" {
			memory, ok := imports.memory(c.memoryImportBindingKey(i))
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
	if b.c.usesGenericGCExecution() && b.c.genericGCFrameRoots() == nil && !rv64OfficialGCRefEq(b.c) && !rv64GCStructGet(b.c) && !rv64GCStructNull(b.c) && !rv64GCStructPacked(b.c) && !rv64GCStructBasic(b.c) && !rv64GCArrayLocal(b.c) && !rv64GCArrayUniform(b.c) && !rv64GCArrayFixedLocal(b.c) && !rv64GCArrayFixedOfficial(b.c) && !rv64GCArrayDefaultOfficial(b.c) && !rv64GCArrayPackedData(b.c) && !rv64GCArrayReference(b.c) && !rv64GCArrayInitElem(b.c) && !rv64GCArrayInitData(b.c) && !rv64GCArrayFill(b.c) && !rv64GCArrayCopy(b.c) {
		// Generic products outside the validated native root-map slice still do
		// not publish every native reference at allocating helper safepoints. Keep
		// them stable and bounded rather than scanning an incomplete frame. The
		// exact RV64 allocating products have no live native reference across an
		// allocating helper safepoint; ref.eq publishes table roots separately,
		// while the local struct/array products allocate before producing a ref.
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
		collector, mapping, err := b.opts.store.acquireGCCollector(b.opts.startContext, gcConfig, b.c, preferred)
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
			return errors.New("wago: Runtime GC domain has no native identity")
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
	if b.gcDomain != nil && !b.constructionGCDomainLocked {
		b.gcDomain.invocationMu.Lock()
		defer b.gcDomain.invocationMu.Unlock()
	}
	return gc.BuildNativeInstanceView(b.collector, localTypes)
}

func (b *instanceBuilder) attachImports(cleanup *resourceCleanup) ([]*resolvedGlobalImport, error) {
	for i, displayKey := range b.c.Imports {
		key := b.c.functionImportBindingKey(i)
		switch value := b.imports[key].(type) {
		case *InstanceExport:
			if err := b.functionAttachments.attach(value); err != nil {
				return nil, fmt.Errorf("imported function %q: %w", displayKey, err)
			}
		case *HostFuncRef:
			if i >= len(b.c.importFuncSigs) {
				return nil, fmt.Errorf("imported host funcref %q has no signature", displayKey)
			}
			if err := b.hostAttachments.attach(value, b.opts.store, b.c.importFuncSigs[i], b.collector, b.gcDomainID, b.c, i); err != nil {
				return nil, fmt.Errorf("imported host funcref %q: %w", displayKey, err)
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
		if err := b.globalAttachments.attach(global, b.opts.store, b.collector, cleanup); err != nil {
			return nil, fmt.Errorf("imported global %q.%q: %w", imp.Module, imp.Name, err)
		}
	}
	if b.c.memoryDir != nil {
		for i, def := range b.c.memoryDir.ehTags {
			if def.ImportKey == "" {
				continue
			}
			tag, ok := b.imports.tag(b.c.tagImportBindingKey(i))
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
	var cleanup resourceCleanup
	cleanup.run(b.functionAttachments.detachAll)
	cleanup.run(b.hostAttachments.detachAll)
	cleanup.run(b.globalAttachments.detachAll)
	cleanup.run(b.tableAttachments.detachAll)
	cleanup.run(b.tagAttachments.detachAll)
	if b.registeredInstance != nil && b.registeredInstance.refStore != nil {
		cleanup.run(func() {
			b.registeredInstance.referenceLifetime().notifyStore(b.registeredInstance.refStore, referenceLifetimeClosed)
		})
	}
	if b.collector != nil {
		cleanup.run(func() {
			if b.collectorShared && b.opts.store != nil && (b.registeredInstance == nil || b.registeredInstance.refStore == nil) {
				b.opts.store.releaseUnclaimedGCCollector(b.collector)
			} else if !b.collectorShared {
				b.collector.Close()
			}
		})
	}
	cleanup.finish()
}

type instanceConstructionCleanup struct {
	resourceCleanup
	primaryFailure bool // terminal observer/start failure stays primary
}

func (b *instanceBuilder) instantiate(mappingOwner *Compiled) (result *Instance, err error) {
	// The body boundary preserves a panic that started before failure teardown.
	// All deferred ownership classes share one journal, so later cleanup panics
	// cannot replace the first one and cannot skip another class's release.
	var body resourceCleanup
	var cleanup instanceConstructionCleanup
	body.run(func() { result, err = b.instantiateWithCleanup(mappingOwner, &cleanup) })
	body.finish()
	if cleanup.panicked && cleanup.primaryFailure {
		return nil, joinPrimary(err, fmt.Errorf("wago: instantiation cleanup: %w", ErrCallbackPanic))
	}
	cleanup.finish()
	return result, err
}

func (b *instanceBuilder) instantiateWithCleanup(mappingOwner *Compiled, cleanup *instanceConstructionCleanup) (result *Instance, err error) {
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
	syncMode := c.importsRequireSync(imports, opts.forceSyncHost || railshotNativeImportArenaBytes(c, imports) != 0)
	if syncMode {
		// A forced host mode can request a protocol the target cannot implement,
		// even for an import-free module. Reject before footprint helpers whose
		// infallible sizes assume the native host frame was already admitted.
		if _, err := runtime.HostCtrlFrameBytesForSlots(int(c.syncHostSlots)); err != nil {
			return nil, wrapContextError("instantiate: synchronous host protocol", err)
		}
	}
	arenaNeed := c.arenaNeedForImports(imports, syncMode)
	if c.memoryCount() == 1 && (opts.memoryLimitPages != 0 || c.memoryImport != "" && c.threadedMemory0()) {
		if arenaNeed > maxInt()-abi.MemoryDirEntryBytes {
			return nil, errors.New("instance metadata footprint overflows memory policy directory")
		}
		arenaNeed += abi.MemoryDirEntryBytes
	}
	metadataNeed := arenaNeed + railshotNativeImportMetadataBytes(c, imports)
	if metadataNeed < arenaNeed {
		return nil, errors.New("instance metadata footprint overflows native import certificates")
	}
	if opts.maxInstanceMetadataBytes != 0 && uint64(metadataNeed) > opts.maxInstanceMetadataBytes {
		return nil, &runtime.ResourceLimitError{
			Resource:  "instance metadata bytes",
			Scope:     "instance",
			Requested: uint64(metadataNeed),
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
			cleanup.run(func() { _ = gcExternConversion.close() })
		}
	}()
	defer func() {
		if !b.success {
			cleanup.run(b.rollbackPreparedState)
		}
	}()
	importGlobals, err := b.attachImports(&cleanup.resourceCleanup)
	if err != nil {
		return nil, err
	}
	if err := railshotPrepareTableDependencies(b, &cleanup.resourceCleanup); err != nil {
		return nil, err
	}
	var globalRefs []importGlobalReference
	defer func() {
		if !b.success {
			cleanup.run(func() { railshotReleaseGlobalReferences(globalRefs) })
		}
	}()
	globalRefs, err = railshotFreezeGlobalReferences(c, importGlobals, opts.store, &cleanup.resourceCleanup)
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
	defer func() {
		if !b.success {
			cleanup.run(func() { runtime.ReleaseEngine(eng) })
		}
	}()
	// Memory: a host-imported *Memory if the module imports one, otherwise an
	// instance-owned mapping (guard-page-backed for signals-based modules, so the
	// fault handler catches OOB accesses through the normal Invoke path).
	var (
		jm              *runtime.JobMemory
		memObj          *Memory
		ownsMem         bool
		threadedControl bool
		ownsControl     bool
		memoryObjs      []*Memory
		memoryOwns      []bool
	)
	var memoryAttachments memoryImportAttachments
	defer func() {
		if !b.success {
			cleanup.run(memoryAttachments.detachAll)
		}
	}()
	attachMemory := func(memory *Memory) error { return memoryAttachments.attach(memory, &cleanup.resourceCleanup) }
	// The primary control mapping is a separate unit from imported memory 0,
	// even when the module has multiple memories. Zero-memory modules still own
	// their primary control mapping. Consume every owned bit before release.
	defer func() {
		if b.success {
			return
		}
		for i := len(memoryObjs) - 1; i >= 1; i-- {
			if memoryObjs[i] != nil && memoryOwns[i] {
				memory := memoryObjs[i]
				memoryOwns[i] = false
				cleanup.run(func() { runtime.ReleaseJobMemory(memory.jobMemory()) })
			}
		}
		if ownsMem || ownsControl {
			ownsMem, ownsControl = false, false
			cleanup.run(func() { runtime.ReleaseJobMemory(jm) })
		}
	}()
	if c.memoryImport != "" {
		m, ok := imports.memory(c.memoryImportBindingKey(0))
		if !ok {
			return nil, fmt.Errorf("missing imported memory %q", c.memoryImport)
		}
		if def, ok := c.memoryImportAt(0); ok {
			if err := m.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64, def.Shared); err != nil {
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
			return nil, fmt.Errorf("imported memory %q is not guard-page backed; signals-based bounds checks require a guard-page memory (build with -tags wago_guardpage)", c.memoryImport)
		}
		if err := attachMemory(m); err != nil {
			return nil, fmt.Errorf("imported memory %q: %w", c.memoryImport, err)
		}
		memObj = m
		if c.threadedMemory0() {
			jm, err = runtime.AcquireJobMemoryGrowable(0, 0)
			if err != nil {
				return nil, wrapContextError("allocate threaded instance control", err)
			}
			threadedControl, ownsControl = true, true
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

	for i := 1; i < memoryCount; i++ {
		def := c.memoryDef(i)
		if def.ImportKey != "" {
			memory, ok := imports.memory(c.memoryImportBindingKey(i))
			if !ok {
				return nil, fmt.Errorf("missing imported memory %q", def.ImportKey)
			}
			if err := memory.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64, def.Shared); err != nil {
				return nil, fmt.Errorf("imported memory %q limits: %w", def.ImportKey, err)
			}
			if err := attachMemory(memory); err != nil {
				return nil, fmt.Errorf("imported memory %q: %w", def.ImportKey, err)
			}
			memoryObjs[i] = memory
			continue
		}
		maxPages, reservationErr := ownedMemoryReservationPages(def)
		if reservationErr != nil {
			return nil, fmt.Errorf("memory %d: %w", i, reservationErr)
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
			return nil, fmt.Errorf("memory %d: %w", i, allocErr)
		}
		memoryObjs[i] = &Memory{jm: secondaryJM}
		memoryOwns[i] = true
	}
	var nativeMemoryDir []byte
	var nativeTagIDs []byte
	ar, err := runtime.AcquireArena(arenaNeed)
	if err != nil {
		return nil, err
	}
	base, err := mappingOwner.acquireCode()
	if err != nil {
		cleanup.run(func() { runtime.ReleaseArena(ar) })
		return nil, err
	}
	var tailInstance *Instance
	var thunkMem []byte // instance-specific HostFuncRef thunks; unmapped on failure/close
	defer func() {
		if b.success {
			return
		}
		if tailInstance != nil {
			cleanup.run(func() { railshotReleaseNativeTail(tailInstance) })
		}
		if thunkMem != nil {
			cleanup.run(func() { _ = c.releaseOwnedCodeMapping(thunkMem) })
		}
		cleanup.run(c.releaseCode)
		cleanup.run(func() { runtime.ReleaseArena(ar) })
	}()
	// An ordinary imported memory shares its basedata with the provider and
	// other importers. Construction must own the same lease as native entries,
	// including active data writes. Start from an empty instance context so
	// absent metadata cannot inherit another tenant's arena pointers.
	var finishMemoryContext func(bool)
	if (c.memoryImport != "" || c.nativeTableContextABI && c.tableImportCount() != 0) && !threadedControl {
		// Shared collector activations acquire domain then native execution.
		// Keep construction in that order and avoid relocking in view creation.
		if b.gcDomain != nil {
			b.gcDomain.invocationMu.Lock()
			b.constructionGCDomainLocked = true
		}
		nativeExecutionMu.Lock()
		var previous, empty [runtime.InstanceContextBytes]byte
		previousOrigin := railshotNativeContextIdentity(jm)
		jm.CaptureInstanceContextBytes(previous[:])
		binary.LittleEndian.PutUint64(previous[runtime.InstanceContextGCNativeViewOffset:], uint64(jm.GCNativeViewPtr()))
		finishMemoryContext = func(initialized bool) {
			if !initialized {
				jm.BindInstanceContextBytes(previous[:])
				railshotSetNativeContextIdentity(jm, previousOrigin)
			}
			nativeExecutionMu.Unlock()
			if b.constructionGCDomainLocked {
				b.constructionGCDomainLocked = false
				b.gcDomain.invocationMu.Unlock()
			}
		}
		// Restore and unlock before failure cleanup frees new arena pointers or
		// detaches the final resource root. Success releases before callbacks.
		defer func() {
			if finishMemoryContext != nil {
				finishMemoryContext(false)
			}
		}()
		// The backing may have grown while this constructor waited for a lease.
		if err := enforceMemoryPageQuota(c, imports, opts.memoryLimitPages); err != nil {
			return nil, err
		}
		jm.BindInstanceContextBytes(empty[:])
		railshotSetNativeContextIdentity(jm, 0)
	}
	nativeContext := ar.AllocNoZero(railshotNativeInvocationContextBytes)
	clear(nativeContext[runtime.InstanceContextBytes:])
	nativeContextPtr := uintptr(unsafe.Pointer(&nativeContext[0]))
	if needsMemoryDir {
		nativeMemoryDir = ar.Alloc(memoryCount * abi.MemoryDirEntryBytes)
		for i, memory := range memoryObjs {
			memoryJM := memory.jobMemory()
			if memoryJM == nil {
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
				tag, ok := imports.tag(c.tagImportBindingKey(i))
				if !ok {
					return nil, fmt.Errorf("imported tag %q is unavailable during native identity setup", def.ImportKey)
				}
				identity = tag.identityValue()
			}
			binary.LittleEndian.PutUint64(nativeTagIDs[i*8:], identity)
		}
		jm.SetEHTagDirPtr(uintptr(unsafe.Pointer(&nativeTagIDs[0])))
	}
	// The builder now owns a compiled-code reference. Release the Module lease
	// before lifecycle and start callbacks; Module.Close may close the Compiled
	// while this reference keeps its native mapping live.
	b.releaseModuleUse()
	var hostLog, ctrl []byte
	var hostEvents *hostEventBindings
	var syncHosts []syncHostBinding
	if syncMode {
		// Synchronous host-call path: install the control frame (not the async
		// log) as the import ctx. Modules that accept public funcrefs and can call
		// them indirectly also need this frame so an owned host descriptor remains
		// callable after crossing from another instance.
		ctrl = ar.AllocNoZero(c.hostCtrlFrameBytes())
		if err := runtime.InitHostCtrlFrame(ctrl); err != nil {
			return nil, wrapContextError("instantiate: initialize host control frame", err)
		}
		jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
		if len(c.Imports) > 0 {
			syncHosts, err = c.buildSyncHosts(imports)
			if err != nil {
				return nil, wrapContextError("instantiate", err)
			}
		}
	} else if len(c.Imports) > 0 {
		hasHostImport := false
		for i, displayKey := range c.Imports {
			key := c.functionImportBindingKey(i)
			if _, cross := imports[key].(*InstanceExport); cross {
				continue
			}
			hasHostImport = true
			if imports[key] == nil {
				return nil, fmt.Errorf("import %q: deferred host event is missing", displayKey)
			}
			if i >= len(c.importFuncSigs) {
				return nil, fmt.Errorf("import %q: missing signature", displayKey)
			}
			if _, err := bindI32HostEvent(imports[key], c.importFuncSigs[i]); err != nil {
				return nil, err
			}
		}
		if hasHostImport {
			hostEvents, err = c.buildHostEvents(imports)
			if err != nil {
				return nil, wrapContextError("instantiate", err)
			}
			// The log's count header is reset at the start of every Invoke and its
			// body is written by native code before deferred callbacks read it, so
			// the ~64 KiB buffer needs no instantiate-time zero-fill.
			hostLog = ar.AllocNoZero(runtime.HostCallLogBytes)
			jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))
			for _, global := range c.GlobalImports {
				if valTypeMayCarryFuncref(global.Type) {
					return nil, wrapContextError("deferred host-event instance cannot import a funcref global", ErrPermissionDenied)
				}
			}
		}
	}
	jm.SetStackFence(eng.StackLimit()) // trap runaway recursion instead of faulting

	var nativeImportConstruction railshotNativeImportConstruction
	sharedThunkBase, sharedThunkOffsets, ownedThunkAddr, generatedThunks, err := buildHostFuncThunks(c, imports, syncMode, ar, jm.LinMemBase(), nativeContextPtr, &nativeImportConstruction)
	if err != nil {
		return nil, err
	}
	thunkMem = generatedThunks
	if c.dynamicImports && c.NumImports > 0 {
		dispatch := ar.Alloc(c.NumImports * runtime.ImportDispatchEntryBytes)
		selfLinMem := uint64(jm.LinMemBase())
		for i, displayKey := range c.Imports {
			key := c.functionImportBindingKey(i)
			off := i * runtime.ImportDispatchEntryBytes
			if ex, ok := imports[key].(*InstanceExport); ok && ex != nil && ex.inst != nil {
				if address := ownedThunkAddr[uint32(i)]; address != 0 {
					binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCodePtrOffset:], address)
					binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchHomeLinMemOffset:], selfLinMem)
					binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchTargetContextOffset:], uint64(nativeContextPtr))
					binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCallerContextOffset:], uint64(nativeContextPtr))
					continue
				}
				if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
					return nil, fmt.Errorf("cross-instance import %q references an unavailable function", displayKey)
				}
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCodePtrOffset:], uint64(ex.inst.base)+uint64(ex.inst.c.Entry[ex.localIdx]))
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchHomeLinMemOffset:], uint64(ex.inst.jm.LinMemBase()))
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchTargetContextOffset:], uint64(ex.inst.nativeContext))
				binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCallerContextOffset:], uint64(nativeContextPtr))
				continue
			}
			addr := uint64(0)
			if ownedThunkAddr != nil {
				addr = ownedThunkAddr[uint32(i)]
			}
			ordinaryGoThunk := addr == 0
			if addr == 0 && i < len(sharedThunkOffsets) && sharedThunkOffsets[i] != noHostThunkOffset {
				addr = uint64(sharedThunkBase) + uint64(sharedThunkOffsets[i])
			}
			if addr == 0 {
				return nil, fmt.Errorf("import %q has no host dispatch thunk", displayKey)
			}
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCodePtrOffset:], addr)
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchHomeLinMemOffset:], selfLinMem)
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchTargetContextOffset:], uint64(nativeContextPtr))
			callerContext := uint64(nativeContextPtr)
			if (goruntime.GOARCH == "arm64" || goruntime.GOARCH == "amd64") && c.supportsGoHostDispatchTag() && syncMode && ordinaryGoThunk {
				if callerContext&runtime.ImportDispatchCallerGoHostTag != 0 {
					return nil, fmt.Errorf("unaligned native caller context")
				}
				callerContext |= runtime.ImportDispatchCallerGoHostTag
			}
			binary.LittleEndian.PutUint64(dispatch[off+runtime.ImportDispatchCallerContextOffset:], callerContext)
		}
		jm.SetImportDispatchPtr(uintptr(unsafe.Pointer(&dispatch[0])))
	}

	var initErr error
	var tableDesc []byte
	var funcRefDescs []byte
	var importRefEntries []importReferenceEntry
	var writeTableEntry func([]byte, uint32)
	if c.needsFuncRefDescs() {
		if railshotNativeContextTrapABI() {
			importRefEntries = make([]importReferenceEntry, c.NumImports)
		}
		selfLinMem := uint64(jm.LinMemBase())
		funcRefDescs = ar.Alloc(runtime.FuncRefDescBytes * (len(c.FuncTypeID) + 1))
		// Descriptor zero is the stable home/context anchor saved by EH records.
		binary.LittleEndian.PutUint64(funcRefDescs[runtime.TableEntryHomeLinMemOffset:], uint64(jm.LinMemBase()))
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
			if railshotNonCallableFunction(c, fidx) {
				continue
			}
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
				localRegABI := regABIEnabled && funcSigLocalRegABI(c.Funcs[li])
				stagedTailRegABI := regABIEnabled && c.stagedFeatures().IsEnabled(CoreFeatureTailCall) && (localRegABI || funcSigReferenceResultRegABI(c.Funcs[li]))
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
				if railshotNativeContextTrapABI() {
					// RV64's retained table bridge enters an importer-owned framed
					// wrapper. Its existing thunk performs the host/native transition;
					// a naked foreign entry cannot use the importer's activation.
					addr, ok := hostThunkAddr(fidx, sharedThunkBase, sharedThunkOffsets, ownedThunkAddr)
					if !ok || addr == 0 || addr&15 != 0 {
						return nil, fmt.Errorf("instantiate: RV64 import %d lacks an aligned published wrapper", fidx)
					}
					native, _ := imports[c.functionImportBindingKey(fidx)].(*InstanceExport)
					host, _ := imports[c.functionImportBindingKey(fidx)].(*HostFuncRef)
					importRefEntries[fidx] = importReferenceEntry{address: addr, native: native, host: host}
					code, home, kind = addr, selfLinMem, abi.FuncRefEntryLocalWrapper
				} else if ex, ok := imports[c.functionImportBindingKey(fidx)].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.localIdx < len(ex.inst.c.Entry) {
					code = uint64(ex.inst.base) + uint64(ex.inst.c.Entry[ex.localIdx])
					home = uint64(ex.inst.jm.LinMemBase())
					targetContext = uint64(ex.inst.nativeContext)
					kind = abi.FuncRefEntryCrossInstanceWrapper
				} else if addr, ok := hostThunkAddr(fidx, sharedThunkBase, sharedThunkOffsets, ownedThunkAddr); ok {
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
			if fidx < c.NumImports && !railshotNativeContextTrapABI() {
				// Cross-instance imports reuse the producer's canonical identity when
				// that producer already owns a descriptor arena.
				if ex, ok := imports[c.functionImportBindingKey(fidx)].(*InstanceExport); ok && ex != nil && ex.inst != nil && ex.inst.funcRefDescs != nil {
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
		// Descriptor zero is the stable home/context anchor saved by EH records.
		binary.LittleEndian.PutUint64(funcRefDescs[runtime.TableEntryHomeLinMemOffset:], uint64(jm.LinMemBase()))
		binary.LittleEndian.PutUint64(funcRefDescs[runtime.FuncRefContextOffset:], uint64(nativeContextPtr))
		jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&funcRefDescs[0])))
	}
	var globalCells []*Global
	var instantiationRoots gc.Slots
	// prev is the segment's previous entry (nil for its first value).
	writeElemEntry := func(entry, prev []byte, refType ValType, value RefInit) (err error) {
		rootCompactEntry := normalizedElemRefType(refType) == ValAnyRef || normalizedElemRefType(refType) == ValI31Ref
		defer func() {
			if err == nil && rootCompactEntry && len(entry) >= 4 {
				instantiationRoots = append(instantiationRoots, compactRefRootSlot(entry[:4]))
			}
		}()
		if value.RepeatPrevious {
			if len(prev) != len(entry) {
				return errors.New("repeated element value has no previous entry")
			}
			copy(entry, prev)
			return nil
		}
		if len(value.Expr) != 0 {
			if normalizedElemRefType(refType) != ValAnyRef && normalizedElemRefType(refType) != ValI31Ref && !(normalizedElemRefType(refType) == ValExternRef && needsExternConversion) {
				return fmt.Errorf("GC element expression has incompatible destination %s", refType)
			}
			if len(entry) < 8 {
				return errors.New("GC element expression entry is truncated")
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
					return errors.New("funcref element global descriptor is truncated")
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
				// RV64's retained reference ABI already publishes framed wrappers in
				// canonical descriptors. Its table guards require an unchanged row.
				global := globalCells[value.GlobalIndex]
				if !c.nativeTableReferenceABI && global.owner != nil && global.owner.instance != nil && global.owner.instance.nativeContext != nativeContextPtr {
					producer := global.owner.instance
					if fidx, ok := producer.funcrefDescriptorIndex(bits); ok {
						li := fidx - producer.c.NumImports
						if li >= 0 && li < len(producer.c.Entry) {
							taggedHome, ok := abi.TagFuncRefHome(uint64(producer.jm.LinMemBase()), abi.FuncRefEntryCrossInstanceWrapper)
							if !ok {
								return errors.New("funcref element global producer home collides with descriptor tags")
							}
							binary.LittleEndian.PutUint64(entry[runtime.TableEntryCodePtrOffset:], uint64(producer.base)+uint64(producer.c.Entry[li]))
							binary.LittleEndian.PutUint64(entry[runtime.TableEntryHomeLinMemOffset:], taggedHome)
						}
					}
				}
				return nil
			case ValExternRef, ValAnyRef, ValI31Ref:
				if len(entry) < 8 {
					return errors.New("reference element global entry is truncated")
				}
				binary.LittleEndian.PutUint64(entry, bits)
				return nil
			case ValExnRef:
				if bits != 0 {
					return errors.New("non-null exception element globals require active handler ownership")
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
				return errors.New("externref element contains a non-null initializer")
			}
			clear(entry)
			return nil
		case ValI31Ref:
			if value.Null {
				clear(entry)
				return nil
			}
			if value.FuncIndex&1 == 0 || len(entry) < 8 {
				return errors.New("i31 element contains an invalid immediate")
			}
			binary.LittleEndian.PutUint64(entry, uint64(value.FuncIndex))
			return nil
		case ValFuncRef:
			if value.Null {
				clear(entry)
				return nil
			}
			if writeTableEntry == nil {
				return errors.New("non-null funcref element has no descriptor arena")
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
	var gcRefTestDescriptors [][]byte
	if c.stagedGCStructProduct().requiresRefTableState() {
		gcRefTestDescriptors = make([][]byte, c.tableCount())
	}
	// A failure before Instance cleanup is installed cannot transfer the producer
	// lifetime of funcrefs written into an imported table. Undo those writes
	// while the table attachment and this instance's arena are still live. Clear
	// to null instead of restoring an overwritten descriptor: its old producer
	// may have been released while this construction was in progress.
	type importedFuncrefWrite struct {
		table *Table
		off   int
		value [runtime.TableEntryBytes]byte
	}
	var importedFuncrefWrites []importedFuncrefWrite
	defer func() {
		if b.success {
			return
		}
		for i := len(importedFuncrefWrites) - 1; i >= 0; i-- {
			write := &importedFuncrefWrites[i]
			write.table.mu.Lock()
			if !write.table.closed && write.off+runtime.TableEntryBytes <= len(write.table.desc) {
				entry := write.table.desc[write.off : write.off+runtime.TableEntryBytes]
				// Avoid clearing a later write observed through this handle. Native
				// guest table mutation does not take this lock, so concurrent guest
				// copying remains outside the construction rollback guarantee.
				if bytes.Equal(entry, write.value[:]) {
					clear(entry)
				}
			}
			write.table.mu.Unlock()
		}
	}()
	recordImportedFuncrefWrite := func(table *Table, off int, before [runtime.TableEntryBytes]byte, entry []byte) {
		if table == nil || len(entry) != runtime.TableEntryBytes || bytes.Equal(before[:], entry) {
			return
		}
		write := importedFuncrefWrite{table: table, off: off}
		copy(write.value[:], entry)
		importedFuncrefWrites = append(importedFuncrefWrites, write)
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
			var importedTable *Table
			def := c.tableDef(tableIndex)
			entryBytes := c.tableEntryBytes(tableIndex)
			if importDef, imported := c.tableImportAt(tableIndex); imported {
				// Shared cross-instance table: run on the exporting instance's descriptor
				// only after proving exact type and externref-store compatibility. Aliased
				// declarations attach one importer root while each declaration validates.
				t, ok := imports.table(c.tableImportBindingKey(tableIndex))
				if !ok {
					return nil, fmt.Errorf("missing imported table %q", importDef.Key)
				}
				exact, err := c.tableExactType(tableIndex)
				if err != nil {
					return nil, fmt.Errorf("imported table %q exact type: %w", importDef.Key, err)
				}
				if err := railshotBindTableImport(&b.tableAttachments, t, c.tableElementType(tableIndex), exact, c.Types, opts.store, b.collector, def.Addr64); err != nil {
					return nil, fmt.Errorf("imported table %q: %w", importDef.Key, err)
				}
				desc = t.desc
				importedTable = t
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
					var before [runtime.TableEntryBytes]byte
					if importedTable != nil {
						copy(before[:], desc[off:off+entryBytes])
					}
					writeTableEntry(desc[off:off+entryBytes], def.InitFunc)
					recordImportedFuncrefWrite(importedTable, off, before, desc[off:off+entryBytes])
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
			var importedTable *Table
			if _, imported := c.tableImportAt(int(el.TableIndex)); imported {
				importedTable, _ = imports.table(c.tableImportBindingKey(int(el.TableIndex)))
			}
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
					initErr = wrapContextError("element offset extended expression", err)
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
				var prev []byte
				if k > 0 {
					prev = desc[off-entryBytes : off]
				}
				var before [runtime.TableEntryBytes]byte
				if importedTable != nil && entryBytes == runtime.TableEntryBytes {
					copy(before[:], desc[off:off+entryBytes])
				}
				writeErr := writeElemEntry(desc[off:off+entryBytes], prev, el.RefType, value)
				recordImportedFuncrefWrite(importedTable, off, before, desc[off:off+entryBytes])
				if writeErr != nil {
					initErr = fmt.Errorf("active element segment %d value %d: %w", seg, k, writeErr)
					break
				}
			}
			if initErr != nil {
				break
			}
		}
		if product := c.stagedGCStructProduct(); initErr == nil && product.requiresRefTableState() && c.tableCount() != 0 {
			specs, err := gcRefTestTableSpecs(c, gcRefTestDescriptors)
			if err != nil {
				initErr = err
			} else {
				canonicalTypes, err := b.gcTypeMap.canonicalTypes(product.refTestCanonicalTypes())
				if err != nil {
					initErr = err
				} else {
					gcRefTestTable, initErr = newGCRefTestTableStateFor(b.collector, specs, canonicalTypes)
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
				var prev []byte
				if k > 0 {
					prev = entries[(k-1)*entryBytes : k*entryBytes]
				}
				if err := writeElemEntry(entries[k*entryBytes:(k+1)*entryBytes], prev, el.RefType, value); err != nil {
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

	if initErr == nil && c.activeDataCount() > 0 {
		for seg := 0; seg < c.activeDataCount(); seg++ {
			d := c.activeDataAt(seg)
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
					initErr = wrapContextError("data offset extended expression", err)
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
		return nil, wrapContextError("compiled metadata invalid", err)
	}
	resultsBytes, err := runtime.SlotBytes(c.maxResultSlots)
	if err != nil {
		return nil, wrapContextError("compiled metadata invalid", err)
	}
	serArgs := ar.Alloc(argsBytes)
	results := ar.Alloc(resultsBytes)
	trap := ar.Alloc(runtime.TrapBufferBytes)
	if err := jm.BindTrapCell(trap); err != nil {
		return nil, wrapContextError("bind trap cell", err)
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
			return nil, wrapContextError("instantiate: native GC metadata view", err)
		}
		jm.SetGCNativeViewPtr(uintptr(unsafe.Pointer(gcNativeView)))
	}
	jm.CaptureInstanceContextBytes(nativeContext)
	binary.LittleEndian.PutUint64(nativeContext[runtime.InstanceContextGCDomainOffset:], b.gcDomainID)
	if gcNativeView != nil {
		binary.LittleEndian.PutUint64(nativeContext[runtime.InstanceContextGCNativeViewOffset:], uint64(uintptr(unsafe.Pointer(gcNativeView))))
	}
	if finishMemoryContext != nil && !(c.nativeTableContextABI && c.tableImportCount() != 0) {
		finishMemoryContext(initErr == nil)
		finishMemoryContext = nil
	}
	in := &Instance{
		c: c, eng: eng, jm: jm, memory: memObj, ownsMem: ownsMem, ar: ar, base: base, hostEvents: hostEvents, imports: imports, hostLog: hostLog, syncMode: syncMode, ctrl: ctrl, syncHosts: syncHosts, globals: globals, globalCells: globalCells, tableDescPtr: tableDescPtr, tableDescLen: len(tableDesc), funcRefDescs: funcRefDescs, passiveDataDesc: passiveDataDesc, thunkMem: thunkMem, gc: b.collector, gcTypeMap: b.gcTypeMap, gcNativeView: gcNativeView,
		serArgs: serArgs, results: results, trap: trap, rt: opts.runtime,
		nativeContext:      nativeContextPtr,
		threadedMemoryZero: c.threadedMemory0(),
		moduleIdentity:     opts.moduleIdentity,
	}
	tailInstance = in
	railshotInstallNativeImportConstruction(in, &nativeImportConstruction)
	railshotCaptureNativeTailPolicy(in, opts, metadataNeed)
	if err := railshotPrepareNativeTail(in, imports, sharedThunkBase, sharedThunkOffsets, ownedThunkAddr); err != nil {
		return nil, err
	}
	if len(importRefEntries) != 0 {
		in.ensureImportState().importRefEntries = importRefEntries
	}
	if len(globalRefs) != 0 {
		in.ensureImportState().globalRefEntries = globalRefs
	}
	if codeProfileEnabled {
		b.bindProfileInstance(in)
	}
	if c.boundedHostSegments() && len(syncHosts) > 1 {
		if shapes := boundedMultiHostShapes(syncHosts); shapes != nil {
			in.ensurePluginState().multiHost = &boundedMultiHostLayout{shapes: shapes}
		}
	}
	if opts.InvokeCacheSlots != 0 && opts.InvokeCacheSlots != 4 {
		state := in.ensurePluginState()
		state.invokeCacheSlots = uint8(opts.InvokeCacheSlots)
	}
	if opts.InvokeCacheSlots > 4 {
		extra := opts.InvokeCacheSlots - 4
		in.pluginState.Load().invokeCacheExtra = &invokeCacheOverflow{
			entries: make([]invokeCache, extra),
		}
	}
	if opts.pluginGCImports != nil {
		in.ensureImportState().pluginGCImports = opts.pluginGCImports
	}
	if c.maxResultSlots <= len(in.resultInline) {
		in.resultVals = in.resultInline[:c.maxResultSlots:c.maxResultSlots]
	} else {
		in.resultVals = make([]uint64, c.maxResultSlots)
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
			return nil, wrapContextError("instantiate: register host control frame", err)
		}
		defer func() {
			if !b.success {
				cleanup.run(func() { unregisterHostControl(in) })
			}
		}()
	}
	if !codeProfileEnabled && in.syncMode && c.boundedHostSegments() && len(syncHosts) == 1 &&
		(syncHosts[0].scalarKind == syncHostScalar || syncHosts[0].privateBoundedTypedHost()) &&
		in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		binding := &syncHosts[0]
		slots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
		var err error
		if binding.scalarKind == syncHostScalar {
			_, err = eng.PrepareScalarHostView(runtimebridge.GrantHostScalarCall(), in.serArgs, in.trap, in.results, in.ctrl, slots)
		} else {
			_, err = eng.PrepareScalarHost(runtimebridge.GrantHostScalarCall(), in.serArgs, in.trap, in.results, in.ctrl, slots)
		}
		if err != nil {
			return nil, wrapContextError("instantiate: prepare scalar host bridge", err)
		}
		if prepared := eng.PreparedScalarHost(); prepared != nil && detachedNumericHostEnabled && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64" && armDetachedNumericEnabled) && c.supportsGoHostDispatchTag() && detachedNumericGoDispatch(jm) && c.NumImports == 1 && memoryCount <= 1 && !threadedControl && len(in.globalCells) == 0 && in.tableDescPtr == 0 && in.gc == nil && !c.needsFuncRefContext() && len(in.hostLog) == 0 && syncHosts[0].gate == nil {
			if err := prepared.DetachNumericContext(runtimebridge.GrantHostScalarCall(), jm); err != nil {
				return nil, err
			}
			if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64" && armIntegerNumericEnabled) && integerNumericHostEnabled && c.integerHostContextAllowed() {
				if err := prepared.EnableIntegerGuestContext(runtimebridge.GrantHostScalarCall()); err != nil {
					return nil, err
				}
			}
		}

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
	// Native start execution, including an RV64 native imported start, may carry
	// a raw legacy panic(nil) through the native host bridge. Both profiling and
	// construction teardown must distinguish that panic from a normal error
	// return even though recover reports a nil value.
	nativeStartReturned := true
	finishConstruction := func() {
		// RV64 imported-table publication holds construction through registration.
		// Release the native lease before Close can reenter resource finalization.
		if finishMemoryContext != nil {
			finishMemoryContext(false)
			finishMemoryContext = nil
		}
		recovered := recover()
		if !nativeStartReturned || recovered != nil {
			result = nil
			if panicErr, ok := recovered.(error); ok {
				err = wrapContextError("wago: instantiation panicked after instance creation", panicErr)
			} else {
				err = fmt.Errorf("wago: instantiation panicked after instance creation: %v", recovered)
			}
		}
		cleanup.primaryFailure = cleanup.primaryFailure || err != nil
		if !b.success {
			b.success = true // the normal Close path now owns all instance resources
			cleanup.run(func() { err = joinPrimary(err, in.Close()) })
		}
		cleanup.run(in.endConstruction)
		// Dependency collection may run terminal callbacks during the final
		// construction transition. Preserve a callback failure that this
		// transition already published, without waiting on unrelated invocations
		// or joining the logical Close result a second time.
		cleanup.run(func() { err = joinPrimary(err, in.publishedTerminalCloseResult()) })
	}
	dependencyConstruction := railshotHasBuilderFunctionDependencies(&b.functionAttachments) || railshotHasBuilderMemoryDependencies(&memoryAttachments) || railshotHasBuilderGlobalDependencies(&b.globalAttachments) || railshotHasBuilderTableDependencies(&b.tableAttachments) || railshotHasFrozenGlobalDependencies(globalRefs)
	if opts.runtime == nil && (c.nativeTableContextABI && c.tableImportCount() != 0 || dependencyConstruction) {
		in.beginConstruction(opts.operationReservation)
		defer finishConstruction()
	}
	if opts.runtime != nil {
		in.beginConstruction(opts.operationReservation)
		if err := opts.runtime.registerInstance(in, opts.runtimeReservation); err != nil {
			in.endConstruction()
			return nil, err
		}
		// Runtime construction stays active through terminal observers and rollback.
		defer finishConstruction()
	}
	// Transfer concrete acquisition records only after normal Close owns terminal
	// rollback. Graph adoption waits until the construction native lease is gone.
	railshotInstallFunctionDependencies(in, &b.functionAttachments)
	railshotInstallMemoryDependencies(in, &memoryAttachments)
	railshotInstallGlobalDependencies(in, &b.globalAttachments)
	railshotInstallTableDependencies(in, &b.tableAttachments)
	railshotInstallFrozenGlobalDependencies(in, globalRefs)
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
	if opts.runtime == nil {
		// Active segments and start calls may leave local funcrefs in imported
		// containers even when instantiation fails. Every failure after this point
		// must transfer their producer lifetime before rollback detaches imports.
		defer func() {
			if !b.success && retainProducerRootsInImportedContainersForFinalization(in) {
				b.success = true
				_ = in.Close()
			}
		}()
	}
	if needsExternConversion {
		if in.refStore == nil || gcExternConversion == nil || gcRefTestTable == nil {
			return nil, errors.New("GC extern conversion ownership is unavailable")
		}
		if err := gcRefTestTable.attachConversion(gcExternConversion); err != nil {
			return nil, err
		}
	}
	if opts.runtime == nil && c.HasStart && !(c.nativeTableContextABI && c.tableImportCount() != 0 || dependencyConstruction) {
		// Only transfer direct-start cleanup after reference-store and conversion
		// ownership is established. Callbacks can close this instance, so keep its
		// resources alive until start execution and construction have unwound.
		in.beginConstruction(opts.operationReservation)
		defer finishConstruction()
	}
	if c.nativeTableContextABI && c.tableImportCount() != 0 {
		// Publish producer membership before releasing construction: a host table
		// has no instance owner from which another importer can discover these
		// local canonical descriptors. Keep the ordinary attachment until Close.
		table, _ := imports.table(c.tableImportBindingKey(0))
		redundantRoot := railshotPublishConstructedTableProducer(table, in)
		if finishMemoryContext != nil {
			finishMemoryContext(initErr == nil)
			finishMemoryContext = nil
		}
		if redundantRoot != nil {
			redundantRoot.releaseResourceRoot()
		}
		table.pruneRetainedInstances()
	}
	if initErr != nil {
		return nil, initErr
	}
	if err := railshotAdoptFunctionDependencies(in, finishMemoryContext == nil); err != nil {
		return nil, err
	}
	if err := railshotAdoptMemoryDependencies(in, finishMemoryContext == nil); err != nil {
		return nil, err
	}
	if err := railshotAdoptGlobalDependencies(in, finishMemoryContext == nil); err != nil {
		return nil, err
	}
	if err := railshotAdoptTableDependencies(in, finishMemoryContext == nil); err != nil {
		return nil, err
	}
	if err := railshotAdoptFrozenGlobalDependencies(in, finishMemoryContext == nil); err != nil {
		return nil, err
	}
	if opts.afterCreate != nil {
		// Concrete import receipts are adopted before callbacks can expose in.
		if err := opts.afterCreate(in); err != nil {
			return nil, err
		}
	}

	// Run the start function (() -> ()) now that memory, globals, table, and data
	// are initialized. A trap here aborts instantiation.
	if opts.startContext != nil {
		if err := opts.startContext.Err(); err != nil {
			return nil, err
		}
	}
	if c.HasStart {
		if codeProfileEnabled && in.lifecycleProfile() != nil {
			var span profileSpanToken
			span, opts.startContext = b.beginProfileInitialization(in, opts.startContext)
			defer finishProfileBoundary(span, &err, &nativeStartReturned)
		}
		if c.StartIsImport {
			// Validation guarantees the imported start is () -> (). RV64 native
			// bindings use their retained owned bridge; ordinary hosts use normalized
			// callbacks. Other targets retain their cross-instance start policy.
			if c.StartImportIdx < 0 || c.StartImportIdx >= len(c.Imports) {
				return nil, fmt.Errorf("start import index %d out of range", c.StartImportIdx)
			}
			displayKey := c.Imports[c.StartImportIdx]
			key := c.functionImportBindingKey(c.StartImportIdx)
			if ex, ok := imports[key].(*InstanceExport); ok && ex != nil {
				nativeStartReturned = false
				handled, startErr := railshotInvokeNativeImportedStart(in, c.StartImportIdx, ownedThunkAddr, opts.startContext)
				nativeStartReturned = true
				if handled {
					if startErr != nil {
						return nil, fmt.Errorf("start function %q: %w", displayKey, startErr)
					}
					b.success = true
					return in, nil
				}
				return nil, fmt.Errorf("start function %q is a cross-instance import; cross-instance imported starts are unsupported", displayKey)
			}
			fn, err := bindSyncHostImport(imports[key], FuncSig{})
			if err != nil {
				return nil, fmt.Errorf("start function %q: %w", displayKey, err)
			}
			startErr := func() error {
				if err := in.beginInvocation(); err != nil {
					return err
				}
				defer in.endInvocation()
				id := newInvocationID()
				reservation := in.constructionReservationSnapshot()
				restore := bindHostInvocationContext(offHeapSlicePtr(in.ctrl), hostInvocationContext{id: id, parent: opts.startContext, reservation: reservation})
				defer restore()
				caller := in.beginHostCallScopeReservedWithID(id, reservation)
				if err := callImportedStart(fn, caller); err != nil {
					return err
				}
				return railshotCheckImportedStartCompletion(in, opts.startContext)
			}()
			if startErr != nil {
				return nil, fmt.Errorf("start function %q: %w", displayKey, startErr)
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
			nativeStartReturned = false
			startErr := func() error {
				if err := in.beginInvocation(); err != nil {
					return err
				}
				defer in.endInvocation()
				state := in.lockInvocation(0)
				defer state.unlockInvocation()
				return in.invokeVoidEntry(opts.startContext, startEntry, in.constructionReservationSnapshot())
			}()
			nativeStartReturned = true
			if startErr != nil {
				// Instantiation writes to imported tables are store side effects. If a
				// local funcref remains installed when start traps, the shared table
				// becomes the failed instance's lifetime owner. The table prunes roots
				// no longer present in any slot, so retention stays bounded by its
				// descriptor capacity rather than by failed-instantiation count.
				return nil, wrapContextError("start function trapped", startErr)
			}
		}
	}

	b.success = true
	return in, nil
}

func callImportedStart(fn syncHostBinding, caller instanceHostModule) (err error) {
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	completed := false
	defer func() {
		recovered := recover()
		if !completed || recovered != nil {
			switch value := recovered.(type) {
			case HostTrap:
				err = value.Err
				if err == nil {
					err = fmt.Errorf("wago: host trapped without an error")
				}
			case *HostTrap:
				if value == nil || value.Err == nil {
					err = fmt.Errorf("wago: host trapped without an error")
				} else {
					err = value.Err
				}
			case HostExit:
				err = &ExitError{Code: value.Code}
			case *HostExit:
				if value == nil {
					err = errors.New("host start panicked with a nil *HostExit")
				} else {
					err = &ExitError{Code: value.Code}
				}
			case error:
				err = wrapContextError("host start panicked", value)
			default:
				err = fmt.Errorf("host start panicked: %v", value)
			}
		}
	}()
	fn.call(caller, nil, nil)
	completed = true
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
	if len(sig.Results) > preparedDirectWideMaxArgs && !(preparedDirectFloatSupported && len(sig.Results) <= 8 && preparedDirectFloatSignature(sig)) ||
		len(sig.Results) > 2 && !preparedDirectWideSupported {
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
	if gp > preparedDirectWideMaxArgs || fp > 8 {
		return false
	}
	for _, t := range sig.Results {
		if t != ValI32 && t != ValI64 && t != ValF32 && t != ValF64 {
			return false
		}
	}
	if len(sig.Results) == 2 && !preparedDirectFloatSupported {
		return (sig.Results[0] == ValI32 || sig.Results[0] == ValI64) &&
			(sig.Results[1] == ValI32 || sig.Results[1] == ValI64)
	}
	if len(sig.Results) > 2 {
		allInt, allFloat := fp == 0, preparedDirectFloatSupported && gp == 0 && len(sig.Results) <= 8
		resGP, resFP := 0, 0
		for _, t := range sig.Results {
			allInt = allInt && (t == ValI32 || t == ValI64)
			allFloat = allFloat && (t == ValF32 || t == ValF64)
			if t == ValF32 || t == ValF64 {
				resFP++
			} else {
				resGP++
			}
		}
		mixed := preparedDirectFloatSupported && len(sig.Results) <= 4 && resGP > 0 && resFP > 0 && resGP <= 2 && resFP <= 2
		return allInt || allFloat || mixed
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
	for _, t := range sig.Params {
		if t != ValI32 && t != ValI64 {
			return false
		}
	}
	for _, t := range sig.Results {
		if t != ValI32 && t != ValI64 {
			return false
		}
	}
	return true
}

// buildHostFuncThunks resolves wrapper-ABI targets for every host-bound import.
// Ordinary targets are immutable and shared with the Compiled code image;
// HostFuncRef dispatch indexes remain instance-specific.
const noHostThunkOffset = -1

func hostThunkAddr(fidx int, sharedBase uintptr, sharedOffsets []int, ownedAddr map[uint32]uint64) (uint64, bool) {
	if ownedAddr != nil {
		if addr := ownedAddr[uint32(fidx)]; addr != 0 {
			return addr, true
		}
	}
	if fidx >= 0 && fidx < len(sharedOffsets) && sharedOffsets[fidx] != noHostThunkOffset {
		return uint64(sharedBase) + uint64(sharedOffsets[fidx]), true
	}
	return 0, false
}

func buildHostFuncThunks(c *Compiled, imports resolvedImports, syncMode bool, arena *runtime.Arena, callerHome, callerContext uintptr, construction *railshotNativeImportConstruction) (sharedBase uintptr, sharedOffsets []int, ownedAddr map[uint32]uint64, ownedMem []byte, err error) {
	railshotInitNativeImportConstruction(construction, c, imports)
	var blob []byte
	offs := map[uint32]int{}
	needsShared := false
	for fidx := 0; fidx < c.NumImports; fidx++ {
		displayKey := c.Imports[fidx]
		key := c.functionImportBindingKey(fidx)
		if export, isCross := imports[key].(*InstanceExport); isCross {
			code, handled, nativeErr := railshotNativeImportCode(c, export, arena, callerHome, callerContext, fidx, construction)
			if nativeErr != nil {
				return 0, nil, nil, nil, nativeErr
			}
			if handled {
				blob = railshotAlignNativeImportBlob(blob)
				offs[uint32(fidx)] = len(blob)
				railshotRecordNativeImportSpan(construction, fidx, len(blob), len(code))
				blob = append(blob, code...)
			}
			continue // cross-instance funcref, not a host function
		}
		if syncMode {
			if owner, ok := imports[key].(*HostFuncRef); ok && owner != nil {
				if fidx >= len(c.importFuncSigs) {
					return 0, nil, nil, nil, fmt.Errorf("import %q wrapper signature is missing", displayKey)
				}
				sig := c.importFuncSigs[fidx]
				paramSlots, slotErr := valTypesSlots(sig.Params)
				if slotErr != nil {
					return 0, nil, nil, nil, fmt.Errorf("import %q wrapper params: %w", displayKey, slotErr)
				}
				resultSlots, slotErr := valTypesSlots(sig.Results)
				if slotErr != nil {
					return 0, nil, nil, nil, fmt.Errorf("import %q wrapper results: %w", displayKey, slotErr)
				}
				owner.mu.Lock()
				dispatchIndex := owner.dispatchIndex
				owner.mu.Unlock()
				if binding, ok := owner.dispatchBinding(c, fidx); ok {
					dispatchIndex = binding.dispatchIndex
				}
				blob = railshotAlignNativeImportBlob(blob)
				offs[uint32(fidx)] = len(blob)
				code, err := railshotOwnedHostThunk(construction, fidx, hostFuncRefDispatchBit|dispatchIndex, paramSlots, resultSlots)
				if err != nil {
					return 0, nil, nil, nil, err
				}
				railshotRecordNativeImportSpan(construction, fidx, len(blob), len(code))
				blob = append(blob, code...)
				continue
			}
			needsShared = true
			continue
		}
		if !isHostCallback(imports[key]) {
			if imports[key] != nil {
				return 0, nil, nil, nil, fmt.Errorf("import %q is %T; async host wrappers require wago.I32HostEvent", displayKey, imports[key])
			}
		}
		needsShared = true
	}
	if needsShared {
		sharedBase, sharedOffsets, err = c.sharedHostFuncThunks(syncMode)
		if err != nil {
			return 0, nil, nil, nil, err
		}
	}
	if len(blob) == 0 {
		return sharedBase, sharedOffsets, nil, nil, nil
	}
	mem, base, err := c.mapPrivateThunkCode(blob)
	if err != nil {
		return 0, nil, nil, nil, wrapContextError("host import wrapper thunk", err)
	}
	railshotFinishNativeImportConstruction(construction, mem, base, len(blob))
	c.registerProfileThunks(base, mem[:len(blob)], offs)
	addr := make(map[uint32]uint64, len(offs))
	for fidx, o := range offs {
		addr[fidx] = uint64(base) + uint64(o)
	}
	return sharedBase, sharedOffsets, addr, mem, nil
}

func (c *Compiled) sharedHostFuncThunks(syncMode bool) (uintptr, []int, error) {
	if c.NumImports == 0 {
		return 0, nil, nil
	}
	cacheIndex := 0
	if syncMode {
		cacheIndex = 1
	}
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	memo := c.loadValidateMemo()
	if memo == nil {
		return 0, nil, errors.New("shared host import wrapper thunk: validation metadata is missing")
	}
	if err := c.releaseFailedPublicationMappingsLocked(); err != nil {
		return 0, nil, wrapContextError("release failed host import wrapper thunks", err)
	}
	cache := &memo.hostThunks[cacheIndex]
	if cache.mem != nil {
		return cache.base, cache.offsets, nil
	}
	offsets := make([]int, c.NumImports)
	var construction railshotHostThunkConstruction
	if syncMode {
		railshotInitHostThunkConstruction(&construction, c)
	}
	var blob []byte
	for fidx := 0; fidx < c.NumImports; fidx++ {
		offsets[fidx] = noHostThunkOffset
		if syncMode {
			if fidx >= len(c.importFuncSigs) {
				return 0, nil, fmt.Errorf("import %q wrapper signature is missing", c.Imports[fidx])
			}
			sig := c.importFuncSigs[fidx]
			paramSlots, err := valTypesSlots(sig.Params)
			if err != nil {
				return 0, nil, fmt.Errorf("import %q wrapper params: %w", c.Imports[fidx], err)
			}
			resultSlots, err := valTypesSlots(sig.Results)
			if err != nil {
				return 0, nil, fmt.Errorf("import %q wrapper results: %w", c.Imports[fidx], err)
			}
			blob = railshotAlignNativeImportBlob(blob)
			offsets[fidx] = len(blob)
			code, err := railshotCertifiedHostThunk(&construction, fidx, uint32(fidx), paramSlots, resultSlots, false)
			if err != nil {
				return 0, nil, err
			}
			railshotRecordHostThunkSpan(&construction, fidx, len(blob))
			blob = append(blob, code...)
			continue
		}
		offsets[fidx] = len(blob)
		blob = append(blob, railshotHostIndirectThunk(uint32(fidx))...)
	}
	mem, base, err := c.mapThunkCode(blob)
	if err != nil {
		cache.mem, cache.base, cache.offsets = mem, 0, nil
		return 0, nil, wrapContextError("shared host import wrapper thunk", err)
	}
	if codeProfileEnabled {
		profileOffsets := make(map[uint32]int, len(offsets))
		for i, off := range offsets {
			if off >= 0 {
				profileOffsets[uint32(i)] = off
			}
		}
		c.registerProfileThunks(base, mem[:len(blob)], profileOffsets)
	}
	cache.mem, cache.base, cache.offsets = mem, base, offsets
	railshotFinishHostThunkConstruction(&construction, len(blob))
	cache.railshotHostThunkConstruction = construction
	return base, offsets, nil
}
