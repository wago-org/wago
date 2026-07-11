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

// RequireService declares a service dependency with an exact expected type.
func RequireService(reg *Registry, name string) (*ServiceRef, error) {
	if reg == nil || name == "" {
		return nil, fmt.Errorf("wago: invalid service requirement")
	}
	ref := &ServiceRef{name: name}
	reg.requires = append(reg.requires, ref)
	return ref, nil
}
