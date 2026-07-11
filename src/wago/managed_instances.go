package wago

import (
	"context"
	"fmt"
	"sync"
)

// InstanceManager is a plugin-scoped owner for instances created through the
// instance.manage capability. It lets plugins implement bounded pools, workers,
// schedulers, and routers without exposing runtime internals. Runtime.Close
// closes every still-owned instance in reverse plugin load order.
type InstanceManager struct {
	mu        sync.Mutex
	rt        *Runtime
	owner     string
	instances map[*ManagedInstance]struct{}
	closed    bool
}

// ManagedInstance is one instance whose lifetime is owned by an
// InstanceManager. Instance exposes the normal call surface; Close releases the
// ownership record and closes the instance exactly once.
type ManagedInstance struct {
	mu      sync.Mutex
	manager *InstanceManager
	value   *Instance
	closed  bool
}

func newPendingInstanceManager(owner string) *InstanceManager {
	return &InstanceManager{owner: owner, instances: map[*ManagedInstance]struct{}{}}
}

func (m *InstanceManager) activate(rt *Runtime) { m.rt = rt }

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
	m.mu.Unlock()
	in, err := rt.Instantiate(ctx, mod, opts...)
	if err != nil {
		return nil, err
	}
	owned := &ManagedInstance{manager: m, value: in}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = in.Close()
		return nil, fmt.Errorf("wago: instance manager closed during instantiation")
	}
	m.instances[owned] = struct{}{}
	m.mu.Unlock()
	return owned, nil
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
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	in, manager := m.value, m.manager
	m.value, m.manager = nil, nil
	m.mu.Unlock()
	if manager != nil {
		manager.mu.Lock()
		delete(manager.instances, m)
		manager.mu.Unlock()
	}
	if in != nil {
		return in.Close()
	}
	return nil
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
	var first error
	for _, owned := range list {
		if err := owned.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
