package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// InstanceManager is a plugin-scoped owner for instances created through the
// instance.manage authority. It lets plugins implement bounded pools, workers,
// schedulers, and routers without exposing runtime internals. Runtime.Close
// closes every still-owned instance in reverse plugin load order.
type InstanceManager struct {
	mu           sync.Mutex
	rt           *Runtime
	owner        string
	instances    map[*ManagedInstance]struct{}
	byInstance   map[*Instance]*ManagedInstance
	closed       bool
	budget       AuthorityScope
	live         uint32
	memoryBytes  uint64
	dispatchMu   sync.Mutex
	dispatchCode *Compiled
	dispatchBase uintptr
	pending      sync.WaitGroup
	draining     []*Instance
}

// ManagedInstance is one instance whose lifetime is owned by an
// InstanceManager. Instance exposes the normal call surface; Close releases the
// ownership record and closes the instance exactly once.
type ManagedInstance struct {
	mu          sync.Mutex
	manager     *InstanceManager
	value       *Instance
	closed      bool
	done        chan struct{}
	closedValue *Instance
	err         error
	memoryBytes uint64
}

var (
	voidFuncType         = wasm.CompType{Kind: wasm.CompFunc}
	voidFuncTypeKeyValue = wasm.StructuralFuncTypeKey(&voidFuncType)
)

func voidFuncTypeKey() uint64 { return voidFuncTypeKeyValue }

func newPendingInstanceManager(owner string, budget AuthorityScope) *InstanceManager {
	return &InstanceManager{owner: owner, budget: budget, instances: map[*ManagedInstance]struct{}{}, byInstance: map[*Instance]*ManagedInstance{}}
}

func (m *InstanceManager) activate(rt *Runtime) {
	m.rt = rt
	rt.managedActive.Store(true)
}

func (m *InstanceManager) caller(caller HostModule) (*Instance, error) {
	h, ok := caller.(instanceHostModule)
	if !ok || !h.valid() || h.in == nil || h.in.rt != m.rt {
		return nil, fmt.Errorf("wago: managed operation requires an active caller: %w", ErrPermissionDenied)
	}
	return h.in, nil
}

// ManagedCaller resolves a caller belonging to an instance owned by this manager.
func (m *InstanceManager) ManagedCaller(caller HostModule) (*ManagedInstance, error) {
	in, err := m.caller(caller)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	owned := m.byInstance[in]
	m.mu.Unlock()
	if owned == nil {
		return nil, fmt.Errorf("wago: caller is not owned by this plugin")
	}
	return owned, nil
}

// CallerIdentity returns only the comparable identity of the active caller.
// It does not grant invocation, close, export, memory, or management access.
func (m *InstanceManager) CallerIdentity(caller HostModule) (InstanceIdentity, error) {
	in, err := m.caller(caller)
	if err != nil {
		return InstanceIdentity{}, err
	}
	return InstanceIdentity{value: in}, nil
}

// WatchCaller returns a channel signaled when caller's synchronous authority
// expires. The cancel function must be called when the watcher is no longer used.
func (m *InstanceManager) WatchCaller(caller HostModule) (<-chan struct{}, func(), error) {
	h, ok := caller.(instanceHostModule)
	if !ok || !h.valid() || h.in == nil || h.in.rt != m.rt {
		return nil, nil, fmt.Errorf("wago: managed operation requires an active caller: %w", ErrPermissionDenied)
	}
	w := &hostCallWaiter{generation: h.generation, wake: make(chan struct{}, 1)}
	if !h.registerWait(w) {
		return nil, nil, fmt.Errorf("wago: caller watcher unavailable")
	}
	return w.wake, func() { h.unregisterWait(w) }, nil
}

// Instantiate creates a runtime-aware instance owned by this plugin service.
func (m *InstanceManager) Instantiate(ctx context.Context, mod *Module, opts ...InstantiateOption) (*ManagedInstance, error) {
	if m == nil {
		return nil, fmt.Errorf("wago: nil instance manager")
	}
	memoryBytes, err := managedMemoryReservation(mod)
	if err != nil {
		return nil, fmt.Errorf("wago: plugin %s module memory limits: %v: %w", m.owner, err, ErrPermissionDenied)
	}
	m.mu.Lock()
	if m.closed || m.rt == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: instance manager is inactive or closed")
	}
	rt := m.rt
	if m.budget.MaxInstances != 0 && m.live >= m.budget.MaxInstances {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: plugin %s managed-instance limit %d reached: %w", m.owner, m.budget.MaxInstances, ErrPermissionDenied)
	}
	if m.budget.MaxMemoryBytes != 0 && memoryBytes > m.budget.MaxMemoryBytes-m.memoryBytes {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: plugin %s module memory total %d; aggregate managed memory reservation %d + %d exceeds budget %d: %w", m.owner, memoryBytes, m.memoryBytes, memoryBytes, m.budget.MaxMemoryBytes, ErrPermissionDenied)
	}
	m.live++
	m.memoryBytes += memoryBytes
	m.pending.Add(1)
	m.mu.Unlock()
	defer m.pending.Done()
	in, err := rt.instantiateOrigin(ctx, mod, InstantiateManaged, true, opts...)
	if err != nil {
		m.mu.Lock()
		m.live--
		m.memoryBytes -= memoryBytes
		m.mu.Unlock()
		return nil, err
	}
	return m.adopt(in, memoryBytes)
}

func managedMemoryReservation(mod *Module) (uint64, error) {
	if mod == nil || mod.c == nil || mod.c.memoryCount() == 0 {
		return 0, nil
	}
	return mod.c.declaredMemoryMaxBytes()
}

func (m *InstanceManager) adopt(in *Instance, memoryBytes uint64) (*ManagedInstance, error) {
	owned := &ManagedInstance{manager: m, value: in, memoryBytes: memoryBytes}
	in.referenceLifetime().afterPhysicalRelease(func() { m.releaseReservation(memoryBytes) })
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		closeErr := in.closeAndWait()
		return nil, joinPrimary(fmt.Errorf("wago: instance manager closed during instantiation"), closeErr)
	}
	m.instances[owned] = struct{}{}
	m.byInstance[in] = owned
	m.mu.Unlock()
	return owned, nil
}

// Fork creates a managed instance of the active caller's module with the same
// safe host functions, by-value globals, GC configuration, and runtime policy.
// Borrowed memories, tables, globals, and cross-instance exports are rejected.
func (m *InstanceManager) Fork(ctx context.Context, caller HostModule) (*ManagedInstance, error) {
	parent, err := m.caller(caller)
	if err != nil {
		return nil, err
	}
	hooks, operation, err := m.rt.beginOperationGeneration("managed Fork", true)
	if err != nil {
		return nil, err
	}
	defer operation.end()
	imports, err := managedForkImports(parent)
	if err != nil {
		return nil, err
	}
	memoryBytes, err := managedMemoryReservation(&Module{c: parent.c})
	if err != nil {
		return nil, fmt.Errorf("wago: plugin %s module memory limits: %v: %w", m.owner, err, ErrPermissionDenied)
	}
	m.mu.Lock()
	if m.closed || m.rt == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: instance manager is inactive or closed")
	}
	if m.budget.MaxInstances != 0 && m.live >= m.budget.MaxInstances {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: plugin %s managed-instance limit %d reached: %w", m.owner, m.budget.MaxInstances, ErrPermissionDenied)
	}
	if m.budget.MaxMemoryBytes != 0 && memoryBytes > m.budget.MaxMemoryBytes-m.memoryBytes {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: plugin %s module memory total %d; aggregate managed memory reservation %d + %d exceeds budget %d: %w", m.owner, memoryBytes, m.memoryBytes, memoryBytes, m.budget.MaxMemoryBytes, ErrPermissionDenied)
	}
	m.live++
	m.memoryBytes += memoryBytes
	m.pending.Add(1)
	rt := m.rt
	m.mu.Unlock()
	defer m.pending.Done()
	rt.mu.Lock()
	bindings := rt.snapshotModuleBindingsLocked(hooks)
	rt.mu.Unlock()
	state := parent.pluginState.Load()
	var gc GCConfig
	hasGC := state != nil && state.gcConfig != nil
	if hasGC {
		gc = *state.gcConfig
	}
	pluginGCImports := parent.pluginGCImportSet()
	mod, err := buildModule(parent.c, bindings)
	var child *Instance
	if err == nil {
		child, err = rt.instantiateWithHooksOrigin(mod, imports, pluginGCImports, gc, hasGC, false, InstantiateManaged, hooks, operation.reservation, nil)
	}
	if err != nil {
		m.mu.Lock()
		m.live--
		m.memoryBytes -= memoryBytes
		m.mu.Unlock()
		return nil, err
	}
	return m.adopt(child, memoryBytes)
}

func managedForkImports(parent *Instance) (Imports, error) {
	imports := make(Imports, len(parent.c.Imports)+len(parent.c.GlobalImports)+2)
	copyImport := func(key string) error {
		v, ok := parent.imports[key]
		if !ok {
			return fmt.Errorf("managed fork import %q is missing", key)
		}
		switch x := v.(type) {
		case HostFunc:
			imports[key] = x
		case GlobalImport:
			if x.Global != nil {
				return fmt.Errorf("managed fork import %q borrows a global: %w", key, ErrManagedImportLifetime)
			}
			imports[key] = x
		default:
			return fmt.Errorf("managed fork import %q has unsafe lifetime type %T: %w", key, v, ErrManagedImportLifetime)
		}
		return nil
	}
	for _, key := range parent.c.Imports {
		if err := copyImport(key); err != nil {
			return nil, err
		}
	}
	for _, imp := range parent.c.GlobalImports {
		if err := copyImport(imp.Module + "." + imp.Name); err != nil {
			return nil, err
		}
	}
	if parent.c.memoryImport != "" {
		if err := copyImport(parent.c.memoryImport); err != nil {
			return nil, err
		}
	}
	if parent.c.tableImport != "" {
		if err := copyImport(parent.c.tableImport); err != nil {
			return nil, err
		}
	}
	return imports, nil
}

// ValidateVoidTableEntry verifies that index is a non-null () -> () funcref.
func (m *ManagedInstance) ValidateVoidTableEntry(index uint32) error {
	in := m.Instance()
	if in == nil {
		return fmt.Errorf("wago: managed instance is closed")
	}
	if err := in.beginInvocation(); err != nil {
		return fmt.Errorf("wago: managed instance validation: %w", err)
	}
	defer in.endInvocation()
	return validateVoidTableEntry(in, index)
}

func validateVoidTableEntry(in *Instance, index uint32) error {
	desc := in.tableDescriptor(0)
	if len(desc) < 8 {
		return fmt.Errorf("wago: instance has no table")
	}
	size := binary.LittleEndian.Uint32(desc)
	if index >= size {
		return fmt.Errorf("wago: table index %d out of bounds (size %d)", index, size)
	}
	off := 8 + int(index)*coreruntime.TableEntryBytes
	if off < 8 || off+coreruntime.TableEntryBytes > len(desc) {
		return fmt.Errorf("wago: table descriptor is truncated")
	}
	entry := desc[off : off+coreruntime.TableEntryBytes]
	if binary.LittleEndian.Uint64(entry) == 0 {
		return fmt.Errorf("wago: table index %d is null", index)
	}
	if got, want := binary.LittleEndian.Uint64(entry[8:]), voidFuncTypeKey(); got != want {
		return fmt.Errorf("wago: table index %d has signature key %d, want () -> () (%d)", index, got, want)
	}
	return nil
}

// InvokeVoidTable invokes a validated () -> () table entry on this managed
// instance. Calls on one instance must be serialized by the owning plugin.
func (m *ManagedInstance) InvokeVoidTable(ctx context.Context, index uint32) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	in, manager := m.value, m.manager
	m.mu.Unlock()
	if in == nil || manager == nil {
		return fmt.Errorf("wago: managed instance is closed")
	}
	if err := in.beginInvocation(); err != nil {
		return fmt.Errorf("wago: managed instance invocation: %w", err)
	}
	defer in.endInvocation()
	if err := validateVoidTableEntry(in, index); err != nil {
		return err
	}
	base, err := manager.ensureVoidDispatcher()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(in.serArgs) < 8 {
		return fmt.Errorf("wago: managed invocation argument buffer is unavailable")
	}
	binary.LittleEndian.PutUint64(in.serArgs, uint64(index))
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if ctx.Done() == nil {
		// Keep the common non-cancelable path identical to the original dispatch:
		// Go 1.22 otherwise retains the context through the trap translation call
		// and adds a heap allocation even though no watcher can ever fire.
		if in.syncMode {
			return in.callNativeSync(base)
		}
		if err := in.callNativeAsync(base, false); err != nil {
			return err
		}
		return in.replayHostLog()
	}
	stopCancel, err := in.startCancellationWatch(ctx, in.trap)
	if err != nil {
		return err
	}
	defer stopCancel()
	if in.syncMode {
		return contextInterruptError(ctx, in.callNativeSyncWithTrapContext(base, in.trap, ctx))
	}
	if err := in.callNativeAsync(base, false); err != nil {
		return contextInterruptError(ctx, err)
	}
	return contextInterruptError(ctx, in.replayHostLog())
}

func (m *InstanceManager) ensureVoidDispatcher() (uintptr, error) {
	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()
	if m.dispatchBase != 0 {
		return m.dispatchBase, nil
	}
	m.mu.Lock()
	closed, rt := m.closed, m.rt
	m.mu.Unlock()
	if closed || rt == nil {
		return 0, fmt.Errorf("wago: instance manager is closed")
	}
	c, err := Compile(rt.cfg, managedVoidDispatcherWasm)
	if err != nil {
		return 0, fmt.Errorf("compile managed call_indirect dispatcher: %w", err)
	}
	base, err := c.acquireCode()
	if err != nil {
		_ = c.Close()
		return 0, err
	}
	if len(c.Entry) != 1 {
		c.releaseCode()
		_ = c.Close()
		return 0, fmt.Errorf("managed dispatcher has %d entries", len(c.Entry))
	}
	m.dispatchCode, m.dispatchBase = c, base+uintptr(c.Entry[0])
	return m.dispatchBase, nil
}

var managedVoidDispatcherWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x08, 0x02, 0x60, 0x00, 0x00, 0x60, 0x01, 0x7f, 0x00,
	0x03, 0x02, 0x01, 0x01,
	0x04, 0x04, 0x01, 0x70, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x11, 0x00, 0x00, 0x0b,
}

// Instance returns the managed runtime instance. It is nil after Close.
func (m *ManagedInstance) Instance() *Instance {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	in := m.value
	m.mu.Unlock()
	return in
}

// Identity returns the opaque comparable identity of this managed instance.
// It is zero after the managed instance has been logically closed.
func (m *ManagedInstance) Identity() InstanceIdentity {
	return InstanceIdentity{value: m.Instance()}
}

func (m *ManagedInstance) Close() error {
	in, err := m.closeLogical()
	if in != nil {
		state := in.ensurePluginState().close.Load()
		if state != nil {
			<-state.quiesced
		}
	}
	return err
}

func (m *ManagedInstance) closeLogical() (*Instance, error) {
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	if m.closed {
		done := m.done
		m.mu.Unlock()
		if done != nil {
			<-done
		}
		m.mu.Lock()
		in, err := m.closedValue, m.err
		m.mu.Unlock()
		return in, err
	}
	m.closed = true
	m.done = make(chan struct{})
	in, manager, done := m.value, m.manager, m.done
	m.closedValue = in
	m.value, m.manager = nil, nil
	m.memoryBytes = 0
	m.mu.Unlock()
	var err error
	if in != nil {
		err = in.Close()
	}
	if manager != nil {
		manager.mu.Lock()
		delete(manager.instances, m)
		delete(manager.byInstance, in)
		manager.mu.Unlock()
	}
	m.mu.Lock()
	m.err = err
	close(done)
	m.mu.Unlock()
	return in, err
}

func (m *InstanceManager) releaseReservation(memoryBytes uint64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.live > 0 {
		m.live--
	}
	if memoryBytes <= m.memoryBytes {
		m.memoryBytes -= memoryBytes
	} else {
		m.memoryBytes = 0
	}
	m.mu.Unlock()
}

func (m *InstanceManager) closeLogical() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	// Phase 1 closes only creation/fork admission. Existing managed instances
	// remain usable by Plugin.Stop; phase 3 closes and drains them afterward.
	return nil
}

func (m *InstanceManager) drain() error {
	if m == nil {
		return nil
	}
	// closed was published under m.mu before this wait, so no later pending.Add
	// can race the Wait. In-flight creators either fail or close their partial
	// instance in adopt before signaling Done.
	m.pending.Wait()
	m.mu.Lock()
	owned := make([]*ManagedInstance, 0, len(m.instances))
	for instance := range m.instances {
		owned = append(owned, instance)
	}
	m.mu.Unlock()
	var errs []error
	list := make([]*Instance, 0, len(owned))
	for _, managed := range owned {
		in, err := managed.closeLogical()
		if err != nil {
			errs = append(errs, err)
		}
		if in != nil {
			list = append(list, in)
		}
	}
	m.mu.Lock()
	list = append(list, m.draining...)
	m.instances = nil
	m.byInstance = nil
	m.mu.Unlock()
	for _, in := range list {
		state := in.ensurePluginState().close.Load()
		if state != nil {
			<-state.quiesced
		}
	}
	m.mu.Lock()
	m.draining = nil
	m.mu.Unlock()
	m.dispatchMu.Lock()
	if m.dispatchCode != nil {
		if err := m.dispatchCode.Close(); err != nil {
			errs = append(errs, err)
		}
		m.dispatchCode.releaseCode()
		m.dispatchCode, m.dispatchBase = nil, 0
	}
	m.dispatchMu.Unlock()
	return errors.Join(errs...)
}

func (m *InstanceManager) close() error {
	return errors.Join(m.closeLogical(), m.drain())
}
