//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func prefinalizerTableWriterSource(two bool) []byte {
	imports, elements := [][]byte{}, [][]byte{}
	count := 1
	if two {
		count = 2
	}
	for i := 0; i < count; i++ {
		name := []string{"first", "second"}[i]
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 1, 0x70, 0, 1))
		elements = append(elements, []byte{2, byte(i), 0x41, 0, 0x0b, 0, 1, 0})
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, []byte{1, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("safe", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(elements...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}))))
}

func TestPhysicalPrefinalizerVisitsLaterTableAfterPruningPanic(t *testing.T) {
	first, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	compile := func(two bool) *Compiled {
		c, err := Compile(nil, prefinalizerTableWriterSource(two))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	old, err := Instantiate(compile(false), NewImports().Table("env", "first", first))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := old.Close(); err != nil || old.resourcesClosed || old.resourceRefs == 0 {
		t.Fatal("old writer must remain rooted by its original live row", err)
	}
	// Public active-element construction replaces the first row. Its old
	// producer root is pruned during the later finalization scan, where the
	// physical callback must not prevent visiting the second live container.
	current, err := Instantiate(compile(true), NewImports().Table("env", "first", first).Table("env", "second", second))
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if old.resourcesClosed {
		t.Fatal("fixture unexpectedly pruned before the finalization boundary")
	}
	callbacks := 0
	old.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("old first-table writer") })
	if err := current.Close(); !errors.Is(err, ErrCallbackPanic) || callbacks != 1 || !old.resourcesClosed || current.finalizing || current.resourcesClosed {
		t.Fatal("first pruning panic lost its outcome or current lifetime", err, callbacks)
	}
	for _, table := range []*Table{first, second} {
		table.mu.Lock()
		rooted := table.retained[current] != nil
		table.mu.Unlock()
		if !rooted {
			t.Fatal("panic skipped a later live table's descriptor owner")
		}
	}
	if err := first.Close(); err != nil || current.resourcesClosed || current.resourceRefs != 1 {
		t.Fatal("second table failed to retain current after first container release", err)
	}
	if err := second.Close(); err != nil || !current.resourcesClosed || current.resourceRefs != 0 || current.jm != nil || current.ar != nil || current.eng != nil {
		t.Fatal("last container did not complete current physical cleanup", err)
	}
}
