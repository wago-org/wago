//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"testing"
)

// Shared-memory importers capture their per-instance basedata pointers and
// rebind them before each serialized native entry. Private globals, funcref
// descriptors, and pure memory computation can therefore coexist safely.
func TestSharedMemoryImporterRebindsBasedataState(t *testing.T) {
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

	// An imported global uses an importer-owned pointer array, rebound on entry.
	withGlobal := mustCompileWat(rt, t, `(module
		(import "env" "mem" (memory 1))
		(import "env" "g" (global (mut i32)))
		(func (export "f") (result i32) (global.get 0)))`)
	globalUser, err := rt.Instantiate(context.Background(), withGlobal, WithImports(Imports{"env.mem": memImport, "env.g": globalImport}))
	if err != nil {
		t.Fatalf("shared-memory importer with imported global: %v", err)
	}
	if err := globalUser.Close(); err != nil {
		t.Fatalf("global importer Close: %v", err)
	}

	// A ref.func user without a table gets an importer-owned descriptor context.
	withFuncref := mustCompileWat(rt, t, `(module
		(import "env" "mem" (memory 1))
		(func $f)
		(elem declare func $f)
		(func (export "g") (result funcref) (ref.func $f)))`)
	funcrefUser, err := rt.Instantiate(context.Background(), withFuncref, WithImports(Imports{"env.mem": memImport}))
	if err != nil {
		t.Fatalf("shared-memory importer using ref.func: %v", err)
	}
	if err := funcrefUser.Close(); err != nil {
		t.Fatalf("funcref importer Close: %v", err)
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

func TestSharedMemoryIndirectCallSwitchesPrivateContext(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()

	producer, err := rt.Instantiate(context.Background(), mustCompileWat(rt, t, `(module
		(import "env" "mem" (memory 1))
		(global $g (mut i32) (i32.const 10))
		(func (export "target") (result i32)
			(global.set $g (i32.add (global.get $g) (i32.const 1)))
			(global.get $g)))`), WithImports(Imports{"env.mem": memory}))
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer producer.Close()
	target, err := producer.ExportedFunc("target")
	if err != nil {
		t.Fatalf("export target: %v", err)
	}

	consumer, err := rt.Instantiate(context.Background(), mustCompileWat(rt, t, `(module
		(type $result-i32 (func (result i32)))
		(import "env" "mem" (memory 1))
		(import "env" "target" (func $target (type $result-i32)))
		(global $g (mut i32) (i32.const 100))
		(table 1 funcref)
		(elem (i32.const 0) func $target)
		(func (export "indirect") (result i32)
			(call_indirect (type $result-i32) (i32.const 0)))
		(func (export "own") (result i32)
			(global.set $g (i32.add (global.get $g) (i32.const 1)))
			(global.get $g)))`), WithImports(Imports{"env.mem": memory, "env.target": target}))
	if err != nil {
		t.Fatalf("instantiate consumer: %v", err)
	}
	defer consumer.Close()

	for _, tc := range []struct {
		export string
		want   int32
	}{{"indirect", 11}, {"own", 101}, {"indirect", 12}, {"own", 102}} {
		got, err := consumer.Invoke(tc.export)
		if err != nil {
			t.Fatalf("%s: %v", tc.export, err)
		}
		if value := AsI32(got[0]); value != tc.want {
			t.Fatalf("%s = %d, want %d", tc.export, value, tc.want)
		}
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
