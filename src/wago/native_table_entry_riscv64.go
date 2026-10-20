//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"strconv"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// A temporary string used only for a map lookup does not escape. Keep common
// names on the stack; cache declared keys once for arbitrarily long names.
// Neither path scans unrelated entries in the supplied imports map. Instance
// metadata is frozen, and Once publishes the cache to all attached call chains.
func rv64NativeFunctionAttachment(owner *Instance, index int) (*InstanceExport, bool) {
	c := owner.c
	ends, _, _, _, _ := c.importModuleEndSections()
	module, name := splitImportKeyAt(c.Imports[index], importModuleEndAt(ends, index))
	var storage [256]byte
	key := strconv.AppendInt(storage[:0], int64(len(module)), 10)
	if len(key)+1+len(module)+len(name) <= len(storage) {
		key = append(key, ':')
		key = append(key, module...)
		key = append(key, name...)
		export, ok := owner.imports[string(key)].(*InstanceExport)
		return export, ok
	}
	if len(c.Imports) > railshot.MaxHostFunctionImports {
		return nil, false
	}
	state := &owner.ensurePluginState().railshotInvocation
	state.longImportKeysOnce.Do(func() {
		state.longImportKeys = make([]string, len(c.Imports))
		for ordinal := range c.Imports {
			state.longImportKeys[ordinal] = c.functionImportBindingKey(ordinal)
		}
	})
	export, native := owner.imports[state.longImportKeys[index]].(*InstanceExport)
	return export, native
}

// Scope storage is reused after a complete entry. Separate bounded slots retain
// an outer activation's targets while a parked callback recursively reenters.
// The initial public invocation also needs a slot, before any host reentry.
const rv64MaxTableEntryDepth = maxHostReentryDepth + 1

const rv64MaxInvocationArgumentOwners = 256

// A scope exists only for a public call with reference arguments. Funcref
// entries own physical roots acquired from the token store; externref entries
// pin their generation-checked slots through native execution and result
// reconciliation. Slots follow tableDepth so parked recursive activations keep
// disjoint owner sets.
type rv64InvocationArgumentOwners struct {
	owners         [rv64MaxInvocationArgumentOwners]*Instance
	externrefs     [rv64MaxInvocationArgumentOwners]uint64
	externrefStore *referenceStore
	count          int
	externrefCount int
	active         bool
}

type railshotPublicFuncrefArguments struct {
	in    *Instance
	scope *rv64InvocationArgumentOwners
	slot  int
}

func rv64PublicReferenceArgumentScopeRequired(in *Instance) bool {
	return in != nil && in.c != nil && (in.c.nativeReferenceCallABI || rv64Core3RefIsNullExecution(in.c) || in.c.nativeReferenceFunctionABI && in.c.nativeContextTrapABI)
}

func railshotBeginPublicFuncrefArguments(in *Instance) (railshotPublicFuncrefArguments, error) {
	if !rv64PublicReferenceArgumentScopeRequired(in) {
		return railshotPublicFuncrefArguments{}, nil
	}
	pool := &in.ensurePluginState().railshotInvocation
	if pool.tableDepth < 0 || pool.tableDepth >= len(pool.argumentOwners) {
		return railshotPublicFuncrefArguments{}, fmt.Errorf("RV64 reference argument owner nesting bound")
	}
	slot := pool.tableDepth
	scope := pool.argumentOwners[slot]
	if scope == nil {
		scope = new(rv64InvocationArgumentOwners)
		pool.argumentOwners[slot] = scope
	}
	if scope.active || scope.count != 0 || scope.externrefCount != 0 || scope.externrefStore != nil {
		return railshotPublicFuncrefArguments{}, fmt.Errorf("RV64 reference argument owner scope is active")
	}
	scope.active = true
	return railshotPublicFuncrefArguments{in: in, scope: scope, slot: slot}, nil
}

func railshotStagePublicExternrefArgument(in *Instance, arguments *railshotPublicFuncrefArguments, token uint64) error {
	if token == 0 || !rv64PublicReferenceArgumentScopeRequired(in) {
		return nil
	}
	pool := &in.ensurePluginState().railshotInvocation
	if arguments == nil || arguments.in != in || arguments.slot < 0 || arguments.slot >= len(pool.argumentOwners) || arguments.slot != pool.tableDepth || arguments.scope == nil || pool.argumentOwners[arguments.slot] != arguments.scope || !arguments.scope.active {
		return fmt.Errorf("RV64 externref argument scope is unavailable")
	}
	scope := arguments.scope
	if in.refStore == nil || scope.externrefStore != nil && scope.externrefStore != in.refStore {
		return fmt.Errorf("RV64 externref argument token store is unavailable")
	}
	if scope.externrefCount == len(scope.externrefs) {
		return fmt.Errorf("RV64 externref argument bound")
	}
	if !in.refStore.pinExternref(token) {
		return fmt.Errorf("RV64 externref argument token is stale or busy")
	}
	scope.externrefStore = in.refStore
	scope.externrefs[scope.externrefCount] = token
	scope.externrefCount++
	return nil
}

func railshotStagePublicFuncrefArgument(in *Instance, arguments *railshotPublicFuncrefArguments, token, descriptor uint64) error {
	if token == 0 || !rv64PublicReferenceArgumentScopeRequired(in) {
		return nil
	}
	pool := &in.ensurePluginState().railshotInvocation
	if arguments == nil || arguments.in != in || arguments.slot < 0 || arguments.slot >= len(pool.argumentOwners) || arguments.slot != pool.tableDepth || arguments.scope == nil || pool.argumentOwners[arguments.slot] != arguments.scope || !arguments.scope.active {
		return fmt.Errorf("RV64 reference argument owner scope is unavailable")
	}
	scope := arguments.scope
	if in.refStore == nil {
		return fmt.Errorf("RV64 reference argument token store is unavailable")
	}
	resolved, owner, ok := in.refStore.resolveFuncrefTokenOwner(token)
	if !ok || owner == nil {
		return fmt.Errorf("RV64 reference argument owner is unavailable")
	}
	if resolved != descriptor {
		owner.releaseResourceRoot()
		return fmt.Errorf("RV64 reference argument descriptor changed")
	}
	if _, valid := rv64PublishedDescriptor(owner, descriptor); !valid {
		owner.releaseResourceRoot()
		return fmt.Errorf("RV64 reference argument descriptor is not a published callable")
	}
	// The token and canonical descriptor identify this exact physical owner.
	// Entry authenticates the retained call graph before native execution.
	for _, existing := range scope.owners[:scope.count] {
		if existing == owner {
			owner.releaseResourceRoot()
			return nil
		}
	}
	if scope.count == len(scope.owners) {
		owner.releaseResourceRoot()
		return fmt.Errorf("RV64 reference argument owner bound")
	}
	scope.owners[scope.count] = owner
	scope.count++
	if owner != in {
		in.markNativeControlShared()
		owner.markNativeControlShared()
	}
	return nil
}

func railshotReleasePublicFuncrefArguments(arguments railshotPublicFuncrefArguments) {
	in, scope := arguments.in, arguments.scope
	if in == nil || scope == nil {
		return
	}
	pool := &in.ensurePluginState().railshotInvocation
	valid := arguments.slot >= 0 && arguments.slot < len(pool.argumentOwners) && pool.argumentOwners[arguments.slot] == scope && pool.tableDepth == arguments.slot && scope.active
	scope.active = false
	var cleanup resourceCleanup
	for scope.externrefCount != 0 {
		scope.externrefCount--
		token := scope.externrefs[scope.externrefCount]
		scope.externrefs[scope.externrefCount] = 0
		store := scope.externrefStore
		cleanup.run(func() {
			if store == nil || !store.unpinExternref(token) {
				panic("RV64 externref argument pin changed")
			}
		})
	}
	scope.externrefStore = nil
	for scope.count != 0 {
		scope.count--
		owner := scope.owners[scope.count]
		scope.owners[scope.count] = nil
		if owner != nil {
			cleanup.run(owner.releaseResourceRoot)
		}
	}
	if !valid {
		cleanup.run(func() { panic("RV64 reference argument owner release scope changed") })
	}
	cleanup.finish()
}

type railshotTableEntryLease struct {
	root    *Instance
	targets *rv64NativeTableTargets
	slot    int
}

func railshotRequiresNativeOwnerScope(c *Compiled) bool {
	return c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.tableCount() != 0 || rv64HasDynamicTail(rv64NativeTailModule(c)) || rv64PrimitiveFunctionGlobalStores(c)
}

func rv64NeedsTableEntry(root *Instance) (bool, error) {
	inline := [8]*Instance{root}
	owners := inline[:1]
	var ownerIndex map[*Instance]struct{}
	edges := 0
	for cursor := 0; cursor < len(owners); cursor++ {
		owner := owners[cursor]
		if owner == nil || owner.c == nil {
			return false, fmt.Errorf("RV64 native attachment has no owner")
		}
		// Typed tails can select canonical descriptors without owning a table.
		// They still need the exact physical owner scope and entry generation.
		if railshotRequiresNativeOwnerScope(owner.c) {
			return true, nil
		}
		for index := range owner.c.Imports {
			edges++
			if edges > 4096 {
				return false, fmt.Errorf("RV64 table entry attachment edge bound")
			}
			export, native := rv64NativeFunctionAttachment(owner, index)
			if !native {
				continue
			}
			if export == nil || export.inst == nil {
				return false, fmt.Errorf("RV64 native attachment is nil")
			}
			var ok bool
			owners, ok = rv64AppendOwnerForWalk(owners, &ownerIndex, export.inst)
			if !ok {
				return false, fmt.Errorf("RV64 table entry owner bound")
			}

		}
	}
	return false, nil
}

// Called while the selected native lease and root invocation gate are held.
// Its returned lease also owns partial acquisitions on failure; release follows
// native unlocking, including failure before the first guest instruction.
func railshotAcquireNativeTableTargets(root *Instance) (railshotTableEntryLease, error) {
	needed, err := rv64NeedsTableEntry(root)
	if err != nil || !needed {
		return railshotTableEntryLease{}, err
	}
	state := root.ensurePluginState()
	pool := &state.railshotInvocation
	if pool.tableDepth != 0 && pool.tableTargets[pool.tableDepth-1].tailRetiring {
		return railshotTableEntryLease{}, fmt.Errorf("RV64 table entry retirement is pending")
	}
	if pool.tableDepth == len(pool.tableTargets) {
		return railshotTableEntryLease{}, fmt.Errorf("RV64 table entry nesting bound")
	}
	slot := pool.tableDepth
	targets := pool.tableTargets[slot]
	if targets == nil {
		targets = new(rv64NativeTableTargets)
		pool.tableTargets[slot] = targets
	}
	if targets.count != 0 || targets.tailRetiring {
		return railshotTableEntryLease{}, fmt.Errorf("RV64 table entry slot is active")
	}
	pool.tableDepth++
	lease := railshotTableEntryLease{root: root, targets: targets, slot: slot}
	if err := targets.refreshWithArguments(root, pool.argumentOwners[slot]); err != nil {
		return lease, err
	}
	return lease, targets.refreshTailGenerations()
}

func railshotReleaseNativeTableTargets(lease railshotTableEntryLease) {
	if lease.targets == nil {
		return
	}
	pool := &lease.root.pluginState.Load().railshotInvocation
	if pool.tableDepth != lease.slot+1 || pool.tableTargets[lease.slot] != lease.targets {
		panic("RV64 table entry release order")
	}
	if lease.targets.tailBudget != (rv64NativeTailGenerationBudget{}) {
		panic("RV64 table entry still owns tail generations")
	}
	pool.tableDepth--
	lease.targets.tailRetiring = false
	lease.targets.release()
}

// Capture at portal entry, while its native lease is still held. Defer arguments
// are evaluated before arbitrary host code runs, so a recursive entry or another
// root's resume cannot select the generation for this parked activation.
func railshotCurrentNativeTableTargets(root *Instance) railshotTableEntryLease {
	if root == nil {
		return railshotTableEntryLease{}
	}
	state := root.pluginState.Load()
	if state == nil || state.railshotInvocation.tableDepth == 0 {
		return railshotTableEntryLease{}
	}
	pool := &state.railshotInvocation
	slot := pool.tableDepth - 1
	return railshotTableEntryLease{root: root, targets: pool.tableTargets[slot], slot: slot}
}

// Every parked-host portal invokes this after its native mutex is reacquired.
// Only this activation's exact entry is refreshed. Suspended outer entries keep
// their original roots and refresh when their own continuation resumes.
func railshotRefreshNativeTableTargets(lease railshotTableEntryLease) {
	if lease.targets == nil {
		return
	}
	pool := &lease.root.pluginState.Load().railshotInvocation
	if lease.slot < 0 || lease.slot >= pool.tableDepth || pool.tableTargets[lease.slot] != lease.targets {
		panic("RV64 table resume lost its entry lease")
	}
	if err := lease.targets.refresh(lease.root); err != nil {
		panic(invalidHostReference{err: err})
	}
	if err := lease.targets.refreshTailGenerations(); err != nil {
		panic(invalidHostReference{err: err})
	}
}

func rv64ActiveTableTrapOwner(root *Instance, identity uintptr) *Instance {
	if root == nil {
		return nil
	}
	state := root.pluginState.Load()
	if state == nil {
		return nil
	}
	pool := &state.railshotInvocation
	for index := pool.tableDepth - 1; index >= 0; index-- {
		if owner := pool.tableTargets[index].owner(identity); owner != nil {
			return owner
		}
	}
	return nil
}

// Physical owner retention also protects stored references. Host control is
// needed only for actual bridge calls or tail dispatch, not storage alone.
func railshotRequiresNativeHostControl(c *Compiled) bool {
	return c != nil && (rv64NativeTailModule(c) != nil || railshot.NativeTableHostControlRequired(c.code))
}

// Resolve frozen table imports with a temporary stack key. Long names use the
// ordinary accessor; no new per-instance cache is needed for the common case.
func rv64NativeTableAttachment(owner *Instance, index int) (*Table, bool) {
	c := owner.c
	_, ends, _, _, _ := c.importModuleEndSections()
	def, ok := c.tableImportAt(index)
	if !ok {
		return nil, false
	}
	module, name := splitImportKeyAt(def.Key, importModuleEndAt(ends, index))
	var storage [256]byte
	key := strconv.AppendInt(storage[:0], int64(len(module)), 10)
	if len(key)+1+len(module)+len(name) > len(storage) {
		return owner.imports.table(c.tableImportBindingKey(index))
	}
	key = append(key, ':')
	key = append(key, module...)
	key = append(key, name...)
	return owner.imports.table(string(key))
}

func railshotNativeFunctionAttachment(in *Instance, index int) (*InstanceExport, bool) {
	return rv64NativeFunctionAttachment(in, index)
}
