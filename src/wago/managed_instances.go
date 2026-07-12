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
// instance.manage capability. It lets plugins implement bounded pools, workers,
// schedulers, and routers without exposing runtime internals. Runtime.Close
// closes every still-owned instance in reverse plugin load order.
type InstanceManager struct {
	mu           sync.Mutex
	rt           *Runtime
	owner        string
	instances    map[*ManagedInstance]struct{}
	byInstance   map[*Instance]*ManagedInstance
	closed       bool
	budget       CapabilityBudget
	live         uint32
	dispatchMu   sync.Mutex
	dispatchCode *Compiled
	dispatchBase uintptr
	pending      sync.WaitGroup
}

// ManagedInstance is one instance whose lifetime is owned by an
// InstanceManager. Instance exposes the normal call surface; Close releases the
// ownership record and closes the instance exactly once.
type ManagedInstance struct {
	mu      sync.Mutex
	manager *InstanceManager
	value   *Instance
	closed  bool
	done    chan struct{}
	err     error
}

var voidFuncType = wasm.CompType{Kind: wasm.CompFunc}

func voidFuncTypeID() uint32 { return wasm.StructuralFuncTypeID(&voidFuncType) }

func newPendingInstanceManager(owner string, budget CapabilityBudget) *InstanceManager {
	return &InstanceManager{owner: owner, budget: budget, instances: map[*ManagedInstance]struct{}{}, byInstance: map[*Instance]*ManagedInstance{}}
}

func (m *InstanceManager) activate(rt *Runtime) {
	m.rt = rt
	rt.managedActive.Store(true)
}

// Caller resolves an active, synchronous host-call capability to its instance.
// The returned pointer is identity only; plugins must not invoke it reentrantly.
func (m *InstanceManager) Caller(caller HostModule) (*Instance, error) {
	h, ok := caller.(instanceHostModule)
	if !ok || !h.valid() || h.in == nil || h.in.rt != m.rt {
		return nil, fmt.Errorf("wago: managed operation requires an active caller: %w", ErrPermissionDenied)
	}
	return h.in, nil
}

// ManagedCaller resolves a caller belonging to an instance owned by this manager.
func (m *InstanceManager) ManagedCaller(caller HostModule) (*ManagedInstance, error) {
	in, err := m.Caller(caller)
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
	if m.budget.MaxMemoryBytes != 0 && mod != nil && mod.c != nil && mod.c.HasMemory && uint64(mod.c.MemMaxPages)*65536 > m.budget.MaxMemoryBytes {
		m.mu.Unlock()
		return nil, fmt.Errorf("wago: plugin %s module memory exceeds managed budget %d: %w", m.owner, m.budget.MaxMemoryBytes, ErrPermissionDenied)
	}
	m.live++
	m.pending.Add(1)
	m.mu.Unlock()
	defer m.pending.Done()
	in, err := rt.instantiateOrigin(ctx, mod, InstantiateManaged, opts...)
	if err != nil {
		m.mu.Lock()
		m.live--
		m.mu.Unlock()
		return nil, err
	}
	return m.adopt(in)
}

func (m *InstanceManager) adopt(in *Instance) (*ManagedInstance, error) {
	owned := &ManagedInstance{manager: m, value: in}
	m.mu.Lock()
	if m.closed {
		if m.live > 0 {
			m.live--
		}
		m.mu.Unlock()
		closeErr := in.Close()
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
	parent, err := m.Caller(caller)
	if err != nil {
		return nil, err
	}
	imports, err := managedForkImports(parent)
	if err != nil {
		return nil, err
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
	m.live++
	m.pending.Add(1)
	rt := m.rt
	m.mu.Unlock()
	defer m.pending.Done()
	state := parent.pluginState.Load()
	var gc GCConfig
	hasGC := state != nil && state.gcConfig != nil
	if hasGC {
		gc = *state.gcConfig
	}
	child, err := rt.instantiateWithHooksOrigin(rt.buildModule(parent.c), imports, gc, hasGC, InstantiateManaged)
	if err != nil {
		m.mu.Lock()
		m.live--
		m.mu.Unlock()
		return nil, err
	}
	return m.adopt(child)
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
	if got, want := binary.LittleEndian.Uint32(entry[8:]), voidFuncTypeID(); got != want {
		return fmt.Errorf("wago: table index %d has signature id %d, want () -> () (%d)", index, got, want)
	}
	return nil
}

// InvokeVoidTable invokes a validated () -> () table entry on this managed
// instance. Calls on one instance must be serialized by the owning plugin.
func (m *ManagedInstance) InvokeVoidTable(ctx context.Context, index uint32) error {
	if err := m.ValidateVoidTableEntry(index); err != nil {
		return err
	}
	in := m.Instance()
	manager := m.manager
	if in == nil || manager == nil {
		return fmt.Errorf("wago: managed instance is closed")
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
	if in.syncMode {
		return in.callNativeSync(base)
	}
	if err := callNative(in.c, in.eng, in.jm, in.nativeControlShared, base, in.serArgs, in.trap, in.results); err != nil {
		return err
	}
	return in.replayHostLog()
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

func (m *ManagedInstance) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		done := m.done
		m.mu.Unlock()
		if done != nil {
			<-done
		}
		m.mu.Lock()
		err := m.err
		m.mu.Unlock()
		return err
	}
	m.closed = true
	m.done = make(chan struct{})
	in, manager, done := m.value, m.manager, m.done
	m.value, m.manager = nil, nil
	m.mu.Unlock()
	var err error
	if in != nil {
		err = in.Close()
	}
	if manager != nil {
		manager.mu.Lock()
		delete(manager.instances, m)
		delete(manager.byInstance, in)
		if manager.live > 0 {
			manager.live--
		}
		manager.mu.Unlock()
	}
	m.mu.Lock()
	m.err = err
	close(done)
	m.mu.Unlock()
	return err
}

func (m *InstanceManager) close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	list := make([]*ManagedInstance, 0, len(m.instances))
	for owned := range m.instances {
		list = append(list, owned)
	}
	m.instances = nil
	m.mu.Unlock()
	// No new creation can increment pending after closed is set under m.mu.
	// Wait for in-flight creations to either fail or close themselves in adopt.
	m.pending.Wait()
	var errs []error
	for _, owned := range list {
		if err := owned.Close(); err != nil {
			errs = append(errs, err)
		}
	}
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
