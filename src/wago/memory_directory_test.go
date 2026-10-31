package wago

import (
	"errors"
	"strings"
	"testing"
)

func TestIndexedMemoryDirectoryValidationAndPolicyAccounting(t *testing.T) {
	base := *newHandBuiltCompiled([]byte{0xc3}, Compiled{
		Exports:          map[string]int{},
		GlobalExports:    map[string]int{},
		HasMemory:        true,
		MemMinPages:      1,
		MemMaxPages:      2,
		requiredFeatures: CoreFeatureMultiMemory,
	})

	t.Run("imports precede locals", func(t *testing.T) {
		c := base
		c.memoryDir = &compiledMemoryDirectory{defs: []memoryDef{{Min: 1, Max: 1, HasMax: true}, {ImportKey: "env.late", Min: 1, Max: 1, HasMax: true}}}
		if err := c.validateMemoryMetadata(c.requiredFeatures); err == nil || !strings.Contains(err.Error(), "follows a local memory") {
			t.Fatalf("validate error = %v", err)
		}
	})

	t.Run("maximum covers minimum", func(t *testing.T) {
		c := base
		c.memoryDir = &compiledMemoryDirectory{defs: []memoryDef{{Min: 2, Max: 1, HasMax: true}}}
		c.requiredFeatures = 0
		if err := c.validateMemoryMetadata(c.requiredFeatures); err == nil || !strings.Contains(err.Error(), "maximum 1 < minimum 2") {
			t.Fatalf("validate error = %v", err)
		}
	})

	t.Run("policy sums all declarations", func(t *testing.T) {
		c := base
		c.memoryDir = &compiledMemoryDirectory{defs: []memoryDef{{Min: 1, Max: 2, HasMax: true}, {Min: 1, Max: 3, HasMax: true}}}
		mod := &Module{c: &c}
		if err := applyPolicy(mod, Policy{MaxMemoryBytes: 5 * 65536}); err != nil {
			t.Fatalf("exact total rejected: %v", err)
		}
		err := applyPolicy(mod, Policy{MaxMemoryBytes: 4 * 65536})
		if !errors.Is(err, ErrPermissionDenied) || !strings.Contains(err.Error(), "total 327680") {
			t.Fatalf("over-budget error = %v", err)
		}
	})
}
