package wago

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledIndexedMemoryDirectoryCodecAndMetadata(t *testing.T) {
	c := &Compiled{
		Code:          []byte{0xc3},
		Exports:       map[string]int{},
		GlobalExports: map[string]int{},
		HasMemory:     true,
		MemMinPages:   1,
		MemMaxPages:   2,
		memoryDir: &compiledMemoryDirectory{
			defs:         []memoryDef{{ImportKey: "env.first", Min: 1, Max: 2, HasMax: true}, {Min: 3, Max: 5, HasMax: true, Shared: true}},
			exports:      map[string]int{"imported": 0, "local": 1},
			exactExports: true,
		},
		requiredFeatures: CoreFeatureMultiMemory,
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if blob[4] != 27 {
		t.Fatalf("codec version = %d, want 27", blob[4])
	}
	var got Compiled
	if err := unmarshalCompiled(&got, blob[5:]); err != nil {
		t.Fatalf("decode codec payload: %v", err)
	}
	got.memoryDir.exactExports = true
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public unsupported multi-memory load error = %v", err)
	}
	if !reflect.DeepEqual(got.memoryDir.defs, c.memoryDir.defs) || !reflect.DeepEqual(got.memoryDir.exports, c.memoryDir.exports) {
		t.Fatalf("memory directory changed: memories=%#v exports=%#v", got.memoryDir.defs, got.memoryDir.exports)
	}
	if got.MemMinPages != 1 || got.MemMaxPages != 2 || !got.HasMemory {
		t.Fatalf("memory-0 cache changed: present=%v min/max=%d/%d", got.HasMemory, got.MemMinPages, got.MemMaxPages)
	}
	if keys := got.MemoryImports(); !reflect.DeepEqual(keys, []string{"env.first"}) {
		t.Fatalf("MemoryImports = %v, want [env.first]", keys)
	}

	meta := (&Module{c: &got}).Metadata()
	if !reflect.DeepEqual(meta.ExportedMemories, []string{"imported", "local"}) || len(meta.Memories) != 2 {
		t.Fatalf("memory metadata = exports %v memories %#v", meta.ExportedMemories, meta.Memories)
	}
	if m := meta.Memories[0]; m.ImportModule != "env" || m.ImportName != "first" || m.Min != 1 || m.Max != 2 || !m.HasMax || !reflect.DeepEqual(m.Exports, []string{"imported"}) {
		t.Fatalf("imported memory metadata = %#v", m)
	}
	if m := meta.Memories[1]; m.ImportModule != "" || m.Min != 3 || m.Max != 5 || !m.Shared || !reflect.DeepEqual(m.Exports, []string{"local"}) {
		t.Fatalf("local memory metadata = %#v", m)
	}
}

func TestIndexedMemoryDirectoryValidationAndPolicyAccounting(t *testing.T) {
	base := Compiled{
		Code:             []byte{0xc3},
		Exports:          map[string]int{},
		GlobalExports:    map[string]int{},
		HasMemory:        true,
		MemMinPages:      1,
		MemMaxPages:      2,
		requiredFeatures: CoreFeatureMultiMemory,
	}

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
