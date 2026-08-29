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
	// MaxMemories caps imported and local memories in the module. 0 means
	// unbounded within the RuntimeConfig limit.
	MaxMemories uint32
	// MaxTableEntries caps the module's table size. 0 means unbounded.
	MaxTableEntries uint32
	// MaxTags caps the number of declared/imported exception tags. 0 means unbounded.
	MaxTags uint32

	// MaxInvokeDuration bounds a single invocation. The low-level call path does
	// not yet implement this bound, so nonzero values are rejected at admission.
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
	if p.MaxInvokeDuration != 0 {
		return fmt.Errorf("maximum invoke duration is not supported by the low-level call path: %w", ErrPermissionDenied)
	}
	for _, cap := range mod.RequiredCapabilities() {
		if !p.allows(cap) {
			return fmt.Errorf("module requires capability %q which the policy does not allow: %w", cap, ErrPermissionDenied)
		}
	}
	if p.MaxMemoryBytes > 0 && mod.c.memoryCount() != 0 {
		maxBytes, err := mod.c.declaredMemoryMaxBytes()
		if err != nil {
			return fmt.Errorf("module memory limits: %v: %w", err, ErrPermissionDenied)
		}
		if maxBytes > p.MaxMemoryBytes {
			return fmt.Errorf("module maximum memory total %d bytes exceeds policy limit %d bytes: %w", maxBytes, p.MaxMemoryBytes, ErrPermissionDenied)
		}
	}
	if p.MaxMemories > 0 && uint64(mod.c.memoryCount()) > uint64(p.MaxMemories) {
		return fmt.Errorf("module memory count %d exceeds policy limit %d: %w", mod.c.memoryCount(), p.MaxMemories, ErrPermissionDenied)
	}
	if p.MaxTableEntries > 0 {
		for i := 0; i < mod.c.tableCount(); i++ {
			capacity := uint64(mod.c.tableRuntimeCapacity(i))
			if declared, ok := mod.c.tableImportAt(i); ok {
				// Imported tables allocate in their provider. Admission can charge
				// only the minimum this module requires, not the provider's declared
				// maximum, which may be sparse table64 metadata.
				capacity = declared.Min
			}
			if capacity > uint64(p.MaxTableEntries) {
				return fmt.Errorf("module table %d capacity %d exceeds policy limit %d: %w", i, capacity, p.MaxTableEntries, ErrPermissionDenied)
			}
		}
	}
	if p.MaxTags > 0 && mod.c.memoryDir != nil && uint32(len(mod.c.memoryDir.ehTags)) > p.MaxTags {
		return fmt.Errorf("module tag count %d exceeds policy limit %d: %w", len(mod.c.memoryDir.ehTags), p.MaxTags, ErrPermissionDenied)
	}
	return nil
}

func applyResolvedTablePolicy(c *Compiled, imports Imports, p Policy) error {
	if p.MaxTableEntries == 0 {
		return nil
	}
	for i := 0; i < c.tableImportCount(); i++ {
		declared, _ := c.tableImportAt(i)
		table, ok := imports.table(declared.Key)
		if !ok {
			continue
		}
		capacity, ok := table.runtimeCapacity()
		if ok && capacity > p.MaxTableEntries {
			return fmt.Errorf("module imported table %d capacity %d exceeds policy limit %d: %w", i, capacity, p.MaxTableEntries, ErrPermissionDenied)
		}
	}
	return nil
}
