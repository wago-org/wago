// Package plugin provides typed helpers for composing Wago plugins.
package plugin

import (
	"fmt"

	"github.com/wago-org/wago"
)

// Contract is a typed, stable ID and incompatible major-version pair.
type Contract[T any] struct {
	id    string
	major uint32
}

func NewContract[T any](id string, major uint32) Contract[T] {
	return Contract[T]{id: id, major: major}
}

func (c Contract[T]) ID() string    { return c.id }
func (c Contract[T]) Major() uint32 { return c.major }
func (c Contract[T]) Spec() wago.ContractSpec {
	return wago.ContractSpec{ID: c.id, Major: c.major}
}

// Ref is a lease-based reference. With is the only supported way to access a
// value: it blocks provider teardown until fn returns and fails closed after
// revocation. The value is valid only for fn; native Go code is trusted to not
// retain and call it after fn returns.
type Ref[T any] struct{ raw *wago.ContractRef }

func (r *Ref[T]) With(fn func(T) error) error {
	if r == nil || r.raw == nil || fn == nil {
		return fmt.Errorf("wago: invalid typed contract call")
	}
	return r.raw.Call(func(value any) error {
		if value == nil {
			var zero T
			return fn(zero)
		}
		typed, ok := value.(T)
		if !ok {
			return fmt.Errorf("wago: contract value %T does not match requested type", value)
		}
		return fn(typed)
	})
}

// OptionalRef reports absence explicitly; zero values are never overloaded as
// the "not provided" signal.
type OptionalRef[T any] struct{ raw *wago.ContractRef }

func (r *OptionalRef[T]) With(fn func(T, bool) error) error {
	if r == nil || r.raw == nil || fn == nil {
		return fmt.Errorf("wago: invalid optional contract call")
	}
	return r.raw.Call(func(value any) error {
		if value == nil {
			var zero T
			return fn(zero, false)
		}
		typed, ok := value.(T)
		if !ok {
			return fmt.Errorf("wago: contract value %T does not match requested type", value)
		}
		return fn(typed, true)
	})
}

// ManyRef leases all implementations for one callback in deterministic provider
// order. The slice is valid only for the duration of fn.
type ManyRef[T any] struct{ raw *wago.ContractRef }

func (r *ManyRef[T]) With(fn func([]T) error) error {
	if r == nil || r.raw == nil || fn == nil {
		return fmt.Errorf("wago: invalid typed contract call")
	}
	return r.raw.CallAll(func(values []any) error {
		typed := make([]T, len(values))
		for i, value := range values {
			var ok bool
			if typed[i], ok = value.(T); !ok {
				return fmt.Errorf("wago: contract value %T does not match requested type", value)
			}
		}
		return fn(typed)
	})
}

func Provide[T any](reg *wago.Registrar, contract Contract[T], value T) error {
	return wago.ProvideContract(reg, contract.Spec(), value)
}

func Require[T any](reg *wago.Registrar, contract Contract[T]) (*Ref[T], error) {
	var witness *T
	raw, err := wago.RequireContract(reg, contract.Spec(), wago.ContractRequired, witness)
	if err != nil {
		return nil, err
	}
	return &Ref[T]{raw: raw}, nil
}

func Optional[T any](reg *wago.Registrar, contract Contract[T]) (*OptionalRef[T], error) {
	var witness *T
	raw, err := wago.RequireContract(reg, contract.Spec(), wago.ContractOptional, witness)
	if err != nil {
		return nil, err
	}
	return &OptionalRef[T]{raw: raw}, nil
}

func Many[T any](reg *wago.Registrar, contract Contract[T]) (*ManyRef[T], error) {
	var witness *T
	raw, err := wago.RequireContract(reg, contract.Spec(), wago.ContractMany, witness)
	if err != nil {
		return nil, err
	}
	return &ManyRef[T]{raw: raw}, nil
}
