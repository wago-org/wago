//go:build ((linux && (amd64 || arm64 || riscv64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"
)

// A module importing a shared memory runs on the memory owner's JobMemory,
// including its fixed negative-offset basedata region. Any per-instance state
// whose pointer lives in basedata (globals array, table pointer, host-call ctx,
// funcref descriptors, passive segments) would clobber a second importer's slot
// and dangle once its arena is freed. Such importers must be rejected; a
// pure-compute importer over the shared linear pages must still succeed.
func TestSharedMemoryImporterRejectsBasedataState(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	owner, err := rt.Instantiate(context.Background(), mustCompileWat(rt, t, `(module
		(memory (export "mem") 1)
		(global (export "g") (mut i32) (i32.const 0)))`))
	if err != nil {
		t.Fatalf("instantiate owner: %v", err)
	}
	defer owner.Close()
	memImport, err := owner.ExportedMemory("mem")
	if err != nil {
		t.Fatalf("ExportedMemory: %v", err)
	}
	globalImport, err := owner.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("ExportedGlobalObject: %v", err)
	}

	// An imported immutable global used only while evaluating an active data
	// offset does not need a native globals pointer after instantiation and may
	// safely initialize the shared pages.
	initOnlyGlobal := mustCompileWat(rt, t, `(module
		(import "env" "g" (global i32))
		(import "env" "mem" (memory 1))
		(data (global.get 0) "a"))`)
	immutableZero := NewGlobalI32(0, false)
	defer immutableZero.Close()
	initializer, err := rt.Instantiate(context.Background(), initOnlyGlobal, WithImports(Imports{"env.mem": memImport, "env.g": immutableZero}))
	if err != nil {
		t.Fatalf("initializer-only shared-memory importer: %v", err)
	}
	if got := memImport.Bytes()[0]; got != 'a' {
		t.Fatalf("active data byte = %q, want a", got)
	}
	if err := initializer.Close(); err != nil {
		t.Fatalf("initializer Close: %v", err)
	}

	// Imported global — the exact reviewer scenario. The importer's globals pointer
	// array is arena-backed and would overwrite the shared basedata GlobalsPtr.
	withGlobal := mustCompileWat(rt, t, `(module
		(import "env" "mem" (memory 1))
		(import "env" "g" (global (mut i32)))
		(func (export "f") (result i32) (global.get 0)))`)
	if _, err := rt.Instantiate(context.Background(), withGlobal, WithImports(Imports{"env.mem": memImport, "env.g": globalImport})); err == nil || !strings.Contains(err.Error(), "shared linear memory") {
		t.Fatalf("shared-memory importer with imported global error = %v, want rejection", err)
	}

	// ref.func user without a table still needs funcref descriptors (basedata slot).
	withFuncref := mustCompileWat(rt, t, `(module
		(import "env" "mem" (memory 1))
		(func $f)
		(elem declare func $f)
		(func (export "g") (result funcref) (ref.func $f)))`)
	if _, err := rt.Instantiate(context.Background(), withFuncref, WithImports(Imports{"env.mem": memImport})); err == nil || !strings.Contains(err.Error(), "shared linear memory") {
		t.Fatalf("shared-memory importer using ref.func error = %v, want rejection", err)
	}

	// Pure computation over the shared linear memory remains allowed.
	pure := mustCompileWat(rt, t, `(module
		(import "env" "mem" (memory 1))
		(func (export "load") (param i32) (result i32) (i32.load8_u (local.get 0))))`)
	consumer, err := rt.Instantiate(context.Background(), pure, WithImports(Imports{"env.mem": memImport}))
	if err != nil {
		t.Fatalf("pure-compute shared-memory importer should succeed: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer Close: %v", err)
	}
}

func mustCompileWat(rt *Runtime, t *testing.T, wat string) *Module {
	t.Helper()
	m, err := rt.Compile(watToWasm(t, wat))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m
}
