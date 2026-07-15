//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func indexedBulkMemoryModule(imported bool) []byte {
	types := wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	))
	sections := [][]byte{types}
	if imported {
		memoryImport := func(name string) []byte {
			entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
			return append(entry, 0x02, 0x01, 0x01, 0x03)
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(memoryImport("m0"), memoryImport("m1"))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(
		wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0),
		wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(3),
	)))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x03}, []byte{0x01, 0x01, 0x03},
		)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init1", 0, 0),
			wasmtest.ExportEntry("copy10", 0, 1),
			wasmtest.ExportEntry("copy01", 0, 2),
			wasmtest.ExportEntry("copy11", 0, 3),
			wasmtest.ExportEntry("fill1", 0, 4),
			wasmtest.ExportEntry("drop", 0, 5),
			wasmtest.ExportEntry("size0", 0, 6),
			wasmtest.ExportEntry("size1", 0, 7),
			wasmtest.ExportEntry("grow1", 0, 8),
			wasmtest.ExportEntry("m0", 2, 0),
			wasmtest.ExportEntry("m1", 2, 1),
		)),
		wasmtest.Section(12, wasmtest.ULEB(2)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x08, 0x00, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x01, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0b, 0x01, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x01, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(
			append([]byte{0x01}, append(wasmtest.ULEB(5), []byte("hello")...)...),
			append([]byte{0x02, 0x01, 0x41, 0x04, 0x0b}, append(wasmtest.ULEB(2), []byte("A1")...)...),
		)),
	)
	return wasmtest.Module(sections...)
}

func exerciseIndexedBulkMemory(t *testing.T, in *Instance) {
	t.Helper()
	m0, err := in.ExportedMemory("m0")
	if err != nil {
		t.Fatalf("export m0: %v", err)
	}
	m1, err := in.ExportedMemory("m1")
	if err != nil {
		t.Fatalf("export m1: %v", err)
	}
	if got := string(m1.Bytes()[4:6]); got != "A1" {
		t.Fatalf("active memory-1 data = %q, want A1", got)
	}
	if _, err := in.Invoke("init1", I32(10), I32(1), I32(3)); err != nil {
		t.Fatalf("memory.init 1: %v", err)
	}
	if got := string(m1.Bytes()[10:13]); got != "ell" {
		t.Fatalf("memory.init bytes = %q, want ell", got)
	}
	copy(m0.Bytes()[20:24], "zero")
	if _, err := in.Invoke("copy10", I32(30), I32(20), I32(4)); err != nil {
		t.Fatalf("memory.copy 0->1: %v", err)
	}
	if got := string(m1.Bytes()[30:34]); got != "zero" {
		t.Fatalf("memory.copy 0->1 bytes = %q", got)
	}
	if _, err := in.Invoke("copy01", I32(40), I32(30), I32(4)); err != nil {
		t.Fatalf("memory.copy 1->0: %v", err)
	}
	if got := string(m0.Bytes()[40:44]); got != "zero" {
		t.Fatalf("memory.copy 1->0 bytes = %q", got)
	}
	copy(m1.Bytes()[50:56], "abcdef")
	if _, err := in.Invoke("copy11", I32(52), I32(50), I32(6)); err != nil {
		t.Fatalf("overlap memory.copy 1->1: %v", err)
	}
	if got := string(m1.Bytes()[50:58]); got != "ababcdef" {
		t.Fatalf("overlap memory.copy bytes = %q, want ababcdef", got)
	}
	if _, err := in.Invoke("fill1", I32(70), I32('x'), I32(5)); err != nil {
		t.Fatalf("memory.fill 1: %v", err)
	}
	if got := string(m1.Bytes()[70:75]); got != "xxxxx" {
		t.Fatalf("memory.fill bytes = %q", got)
	}
	before := append([]byte(nil), m1.Bytes()[80:84]...)
	if _, err := in.Invoke("copy10", I32(65534), I32(20), I32(4)); err == nil {
		t.Fatal("out-of-bounds cross-memory copy unexpectedly succeeded")
	}
	if !bytes.Equal(m1.Bytes()[80:84], before) {
		t.Fatal("trapping cross-memory copy changed unrelated destination bytes")
	}
	if _, err := in.Invoke("drop"); err != nil {
		t.Fatalf("data.drop: %v", err)
	}
	if _, err := in.Invoke("init1", I32(90), I32(0), I32(1)); err == nil {
		t.Fatal("memory.init after data.drop unexpectedly succeeded")
	}
	if _, err := in.Invoke("init1", I32(90), I32(0), I32(0)); err != nil {
		t.Fatalf("zero-length memory.init after data.drop: %v", err)
	}
}

func TestStagedMultiMemoryBulkDataAndAliasLifecycle(t *testing.T) {
	t.Run("local active passive bulk", func(t *testing.T) {
		compiled := stagedMultiMemoryCompile(t, indexedBulkMemoryModule(false))
		if len(compiled.Data) != 1 || compiled.Data[0].MemoryIndex != 1 {
			t.Fatalf("active data metadata = %#v, want memory index 1", compiled.Data)
		}
		blob, err := compiled.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal codec v25: %v", err)
		}
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
			t.Fatalf("decode codec v25 payload: %v", err)
		}
		if len(loaded.Data) != 1 || loaded.Data[0].MemoryIndex != 1 {
			t.Fatalf("loaded active data metadata = %#v, want memory index 1", loaded.Data)
		}
		in, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		defer in.Close()
		exerciseIndexedBulkMemory(t, in)
		if err := validateSnapshotModule(compiled); err == nil || !strings.Contains(err.Error(), "multiple memories") {
			t.Fatalf("snapshot policy error = %v", err)
		}
	})

	t.Run("duplicate imported aliases attach once", func(t *testing.T) {
		compiled := stagedMultiMemoryCompile(t, indexedBulkMemoryModule(true))
		if err := applyPolicy(&Module{c: compiled}, Policy{MaxMemoryBytes: 2 * 3 * 65536}); err != nil {
			t.Fatalf("declaration-total policy rejected exact limit: %v", err)
		}
		if err := applyPolicy(&Module{c: compiled}, Policy{MaxMemoryBytes: 3 * 65536}); err == nil {
			t.Fatal("declaration-total policy accepted one-memory limit for two declarations")
		}
		memory, err := NewMemory(1, 3)
		if err != nil {
			t.Fatalf("NewMemory: %v", err)
		}
		in, err := instantiateCore(compiled, InstantiateOptions{Imports: Imports{"env.m0": memory, "env.m1": memory}})
		if err != nil {
			t.Fatalf("instantiate duplicate memory aliases: %v", err)
		}
		if err := memory.Close(); err == nil || !strings.Contains(err.Error(), "1 live importer") {
			t.Fatalf("live duplicate alias close error = %v, want one importer claim", err)
		}
		exerciseIndexedBulkMemory(t, in)
		if got := tableTestCallI32(t, in, "grow1", I32(1)); got != 1 {
			t.Fatalf("alias memory.grow = %d, want old size 1", got)
		}
		if got0, got1 := tableTestCallI32(t, in, "size0"), tableTestCallI32(t, in, "size1"); got0 != 2 || got1 != 2 {
			t.Fatalf("alias sizes after grow = %d,%d, want 2,2", got0, got1)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close duplicate aliases: %v", err)
		}
		if err := memory.Close(); err != nil {
			t.Fatalf("close host memory after consumer: %v", err)
		}
	})
}
