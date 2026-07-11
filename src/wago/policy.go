package wago

import (
	"fmt"
	"time"
)

// Policy bounds what a module instance may do: which capabilities it may
// exercise and coarse resource limits. The zero Policy is fully permissive.
type Policy struct {
	// AllowedCapabilities, when non-empty, is the exclusive allow-list: a required
	// capability outside it is denied. When empty, all capabilities are allowed
	// (subject to DeniedCapabilities).
	AllowedCapabilities []Capability
	// DeniedCapabilities is always denied and takes precedence over Allowed.
	DeniedCapabilities []Capability

	// MaxMemoryBytes caps the module's maximum linear memory. 0 means unbounded.
	MaxMemoryBytes uint64
	// MaxTableEntries caps the module's table size. 0 means unbounded.
	MaxTableEntries uint32

	// MaxInvokeDuration bounds a single invocation. Accepted but not yet enforced
	// by the low-level call path; reserved.
	MaxInvokeDuration time.Duration
}

// allows reports whether the policy permits a capability.
func (p Policy) allows(cap Capability) bool {
	for _, d := range p.DeniedCapabilities {
		if d == cap {
			return false
		}
	}
	if len(p.AllowedCapabilities) == 0 {
		return true
	}
	for _, a := range p.AllowedCapabilities {
		if a == cap {
			return true
		}
	}
	return false
}

// applyPolicy validates a module against a policy: every capability the module
// requires must be permitted, and its declared limits must fit. It returns an
// error wrapping ErrPermissionDenied on violation. The zero Policy passes.
func applyPolicy(mod *Module, p Policy) error {
	for _, cap := range mod.RequiredCapabilities() {
		if !p.allows(cap) {
			return fmt.Errorf("module requires capability %q which the policy does not allow: %w", cap, ErrPermissionDenied)
		}
	}
	if p.MaxMemoryBytes > 0 && mod.c.HasMemory {
		maxBytes := uint64(mod.c.MemMaxPages) * 65536
		if maxBytes > p.MaxMemoryBytes {
			return fmt.Errorf("module maximum memory %d bytes exceeds policy limit %d bytes: %w", maxBytes, p.MaxMemoryBytes, ErrPermissionDenied)
		}
	}
	if p.MaxTableEntries > 0 {
		for i := 0; i < mod.c.tableCount(); i++ {
			size := mod.c.tableMinimum(i)
			if uint64(size) > uint64(p.MaxTableEntries) {
				return fmt.Errorf("module table %d size %d exceeds policy limit %d: %w", i, size, p.MaxTableEntries, ErrPermissionDenied)
			}
		}
	}
	return nil
}
