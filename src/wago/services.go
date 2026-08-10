package wago

import (
	"fmt"
	"reflect"
	"sync"
)

// ServiceRef is the runtime's type-checked, late-bound service reference. Most
// plugin authors use the generic wrapper in github.com/wago-org/wago/plugin.
type ServiceRef struct {
	mu    sync.RWMutex
	name  string
	typ   reflect.Type
	value any
	bound bool
}

func (r *ServiceRef) Get() (any, error) {
	if r == nil {
		return nil, fmt.Errorf("wago: nil service reference")
	}
	r.mu.RLock()
	value, bound := r.value, r.bound
	r.mu.RUnlock()
	if !bound {
		return nil, fmt.Errorf("wago: service %q is not active", r.name)
	}
	return value, nil
}

func (r *ServiceRef) serviceName() string       { return r.name }
func (r *ServiceRef) serviceType() reflect.Type { return r.typ }
func (r *ServiceRef) bindService(value any) error {
	if value == nil || r.typ != nil && !reflect.TypeOf(value).AssignableTo(r.typ) {
		return fmt.Errorf("wago: service %q has type %T, want %v", r.name, value, r.typ)
	}
	r.mu.Lock()
	r.value, r.bound = value, true
	r.mu.Unlock()
	return nil
}

func (r *ServiceRef) close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.value, r.bound = nil, false
	r.mu.Unlock()
	return nil
}

type serviceBinder interface {
	serviceName() string
	serviceType() reflect.Type
	bindService(any) error
}

type serviceProvision struct {
	name  string
	typ   reflect.Type
	value any
}

// ProvideService publishes a service. Use plugin.Provide for compile-time type
// safety in external plugins.
func ProvideService(reg *Registry, name string, value any) error {
	if reg == nil || name == "" || value == nil {
		return fmt.Errorf("wago: invalid service provision")
	}
	reg.provides = append(reg.provides, serviceProvision{name: name, typ: reflect.TypeOf(value), value: value})
	return nil
}

// RequireService declares a service dependency. An optional pointer type
// witness records the expected type for transactional graph validation; typed
// plugin helpers supply it automatically.
func RequireService(reg *Registry, name string, typeWitness ...any) (*ServiceRef, error) {
	if reg == nil || name == "" {
		return nil, fmt.Errorf("wago: invalid service requirement")
	}
	if len(typeWitness) > 1 {
		return nil, fmt.Errorf("wago: service %q has multiple type witnesses", name)
	}
	var typ reflect.Type
	if len(typeWitness) == 1 {
		witness := reflect.TypeOf(typeWitness[0])
		if witness == nil || witness.Kind() != reflect.Pointer {
			return nil, fmt.Errorf("wago: service %q type witness must be a typed pointer", name)
		}
		typ = witness.Elem()
	}
	ref := &ServiceRef{name: name, typ: typ}
	reg.requires = append(reg.requires, ref)
	if reg.hooks == nil {
		reg.hooks = &HookRegistry{}
	}
	reg.hooks.internalClose = append(reg.hooks.internalClose, ref.close)
	return ref, nil
}
