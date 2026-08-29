package wago

import (
	"fmt"
	"reflect"
	"sync"
)

type contractProvision struct {
	spec  ContractSpec
	typ   reflect.Type
	value any
	owner string
}

type contractSlot struct {
	mu       sync.Mutex
	cond     *sync.Cond
	spec     ContractSpec
	typ      reflect.Type
	values   []any
	active   bool
	revoking bool
	inflight uint64
}

func newContractSlot(spec ContractSpec, typ reflect.Type) *contractSlot {
	s := &contractSlot{spec: spec, typ: typ}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *contractSlot) activate(values []any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active || s.revoking {
		return fmt.Errorf("wago: contract %q v%d is already active", s.spec.ID, s.spec.Major)
	}
	for _, value := range values {
		if value == nil || s.typ != nil && !reflect.TypeOf(value).AssignableTo(s.typ) {
			return fmt.Errorf("wago: contract %q v%d has type %T, want %v", s.spec.ID, s.spec.Major, value, s.typ)
		}
	}
	s.values = append([]any(nil), values...)
	s.active = true
	return nil
}

func (s *contractSlot) acquire(all bool) ([]any, func(), error) {
	if s == nil {
		return nil, nil, fmt.Errorf("wago: nil contract reference")
	}
	s.mu.Lock()
	if !s.active || s.revoking {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: contract %q v%d is not active: %w", s.spec.ID, s.spec.Major, ErrPermissionDenied)
	}
	values := s.values
	if !all && len(values) > 1 {
		values = values[:1]
	}
	s.inflight++
	s.mu.Unlock()
	var once sync.Once
	return values, func() {
		once.Do(func() {
			s.mu.Lock()
			s.inflight--
			if s.inflight == 0 {
				s.cond.Broadcast()
			}
			s.mu.Unlock()
		})
	}, nil
}

// revoke prevents new calls immediately and waits for in-flight calls before
// clearing provider values. It is safe to race with contract calls.
func (s *contractSlot) revoke() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.revoking = true
	s.active = false
	for s.inflight != 0 {
		s.cond.Wait()
	}
	s.values = nil
	s.mu.Unlock()
	return nil
}

//lint:ignore U1000 retained for immediate non-blocking service deactivation
func (s *contractSlot) deactivate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.revoking = true
	s.active = false
	s.mu.Unlock()
}

type contractBinder interface {
	contractSpec() ContractSpec
	contractMode() ContractMode
	contractType() reflect.Type
	contractSlotValue() *contractSlot
}

// ProvideContract publishes one implementation declared by PluginDefinition.
// External callers normally use plugin.Provide for compile-time type safety.
func ProvideContract(reg *Registrar, spec ContractSpec, value any) error {
	if err := reg.ensureOpen(); err != nil {
		return err
	}
	if spec.ID == "" || spec.Major == 0 || value == nil {
		return fmt.Errorf("wago: invalid contract provision")
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if rv.IsNil() {
			return fmt.Errorf("wago: nil contract provision")
		}
	}
	reg.provides = append(reg.provides, contractProvision{spec: spec, typ: reflect.TypeOf(value), value: value, owner: reg.definition.ID})
	return nil
}

// ContractRef is the runtime's lease-based untyped contract reference. There is
// no Get API: Call holds an in-flight lease until fn returns. The value is valid
// only during fn; native Go plugins are trusted to honor that lifetime.
type ContractRef struct {
	spec ContractSpec
	mode ContractMode
	typ  reflect.Type
	slot *contractSlot
}

func (r *ContractRef) contractSpec() ContractSpec       { return r.spec }
func (r *ContractRef) contractMode() ContractMode       { return r.mode }
func (r *ContractRef) contractType() reflect.Type       { return r.typ }
func (r *ContractRef) contractSlotValue() *contractSlot { return r.slot }

// Call invokes fn while holding an active reference lease. Optional references
// call fn with nil when no provider was selected; required references never do.
func (r *ContractRef) Call(fn func(any) error) error {
	if r == nil || r.slot == nil || fn == nil {
		return fmt.Errorf("wago: invalid contract call")
	}
	values, release, err := r.slot.acquire(false)
	if err != nil {
		return err
	}
	defer release()
	if len(values) == 0 {
		return fn(nil)
	}
	return fn(values[0])
}

// CallAll invokes fn once under a single lease with every deterministic provider.
func (r *ContractRef) CallAll(fn func([]any) error) error {
	if r == nil || r.slot == nil || fn == nil {
		return fmt.Errorf("wago: invalid contract call")
	}
	values, release, err := r.slot.acquire(true)
	if err != nil {
		return err
	}
	defer release()
	return fn(append([]any(nil), values...))
}

// RequireContract declares one contract consumption. External callers normally
// use plugin.Require, Optional, or Many for compile-time type safety.
func RequireContract(reg *Registrar, spec ContractSpec, mode ContractMode, typeWitness any) (*ContractRef, error) {
	if err := reg.ensureOpen(); err != nil {
		return nil, err
	}
	if spec.ID == "" || spec.Major == 0 || mode != ContractRequired && mode != ContractOptional && mode != ContractMany {
		return nil, fmt.Errorf("wago: invalid contract requirement")
	}
	witness := reflect.TypeOf(typeWitness)
	if witness == nil || witness.Kind() != reflect.Pointer {
		return nil, fmt.Errorf("wago: contract %q type witness must be a typed pointer", spec.ID)
	}
	ref := &ContractRef{spec: spec, mode: mode, typ: witness.Elem(), slot: newContractSlot(spec, witness.Elem())}
	reg.consumes = append(reg.consumes, ref)
	return ref, nil
}
