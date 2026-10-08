//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

type nativeTailDynamicLease struct {
	site       *tailRADynamicSite
	trampoline uintptr
}

// Bind restores a sealed factory under a reacquired physical owner graph lease.
// The opaque caller snapshot proves the exact context's lifetime independently
// of dynamic targets (which need not include the caller). Imported/host callers
// require a separately qualified ownership adapter. The caller serializes all
// native use and binding changes. The original caller instance must never have
// physically retired; matching reused addresses cannot establish that identity.
// No allocation, publication or lease acquisition
// is needed for an unchanged definition-only graph. All checks precede writes.
func (o *NativeTailDispatcher) Bind(caller *NativeTailDefinedOwner) error {
	if o == nil || !o.ready || o.code == nil || o.code.Base() == 0 || len(o.capability) < tailRACapHeader || len(o.contexts) != 1 {
		return unsupported("native tail factory is not ready to bind")
	}
	if err := caller.Validate(); err != nil {
		return err
	}
	owner := &caller.owner
	if tailRAPointer(owner.context) != tailRAPointer(o.contexts[0]) || owner.functions[0].job.LinMemBase() != o.callerHome {
		return unsupported("native tail binding caller identity")
	}
	for _, retained := range o.dynamic {
		if _, _, err := retained.site.budget(); err != nil {
			return err
		}
		for _, target := range retained.site.owners {
			if err := target.validate(retained.trampoline); err != nil {
				return err
			}
		}
	}
	expected := uint64(0)
	if o.bound {
		expected = uint64(tailRAPointer(o.capability))
	}
	for _, ctx := range o.contexts {
		if binary.LittleEndian.Uint64(ctx[abi.RV64TailCapabilityOffset:]) != expected {
			return unsupported("native tail binding slot is occupied or changed")
		}
	}
	for _, ctx := range o.contexts {
		binary.LittleEndian.PutUint64(ctx[abi.RV64TailCapabilityOffset:], uint64(tailRAPointer(o.capability)))
	}
	o.bound = true
	return nil
}

// Detach removes only this factory's binding and retains its sealed code and
// capability for suspended records or later reuse. The runtime must hold the
// particular caller's physical lease and native serialization. Target refresh
// failure cannot prevent detaching a live caller. Already detached factories do
// not read borrowed contexts: a nested factory may now own the same slot. If
// Detach fails, the caller context must stay alive through the successful retry.
func (o *NativeTailDispatcher) Detach() error {
	if o == nil || !o.bound {
		return nil
	}
	if err := o.CheckDetach(); err != nil {
		return err
	}
	for _, ctx := range o.contexts {
		binary.LittleEndian.PutUint64(ctx[abi.RV64TailCapabilityOffset:], 0)
	}
	o.bound = false
	return nil
}

// CheckDetach authenticates the binding without changing it. Runtime graph
// transitions can validate every affected slot before clearing any of them.
// The particular caller lease and native serialization must be held.
func (o *NativeTailDispatcher) CheckDetach() error {
	if o == nil || !o.bound {
		return nil
	}
	for _, ctx := range o.contexts {
		value := binary.LittleEndian.Uint64(ctx[abi.RV64TailCapabilityOffset:])
		if value != 0 && value != uint64(tailRAPointer(o.capability)) {
			return unsupported("native tail capability binding replaced")
		}
	}
	return nil
}
