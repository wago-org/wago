// Package plugin provides typed helpers for composing Wago plugins.
package plugin

import (
	"fmt"

	"github.com/wago-org/wago"
)

type ServiceKey[T any] struct{ name string }

func NewServiceKey[T any](name string) ServiceKey[T] { return ServiceKey[T]{name: name} }
func (k ServiceKey[T]) Name() string                 { return k.name }

type Ref[T any] struct{ raw *wago.ServiceRef }

func (r *Ref[T]) Get() (T, error) {
	var zero T
	if r == nil || r.raw == nil {
		return zero, fmt.Errorf("wago: nil typed service reference")
	}
	value, err := r.raw.Get()
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("wago: service %T does not match requested type", value)
	}
	return typed, nil
}

func Provide[T any](reg *wago.Registry, key ServiceKey[T], value T) error {
	return wago.ProvideService(reg, key.name, value)
}

func Require[T any](reg *wago.Registry, key ServiceKey[T]) (*Ref[T], error) {
	raw, err := wago.RequireService(reg, key.name)
	if err != nil {
		return nil, err
	}
	return &Ref[T]{raw: raw}, nil
}
