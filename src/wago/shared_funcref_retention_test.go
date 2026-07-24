//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// A funcref written into a shared table embeds the producing instance's code
// pointer and home memory. If the producer closes after a successful table.set,
// the descriptor it left behind must stay backed by live code: the table retains
// the producer until the entry is overwritten or the table closes. Regression
// for the use-after-free where a closed producer's arena was freed while another
// importer could still call_indirect its funcref.
func TestCloseSnapshotsPostHostFuncrefWritesAfterQuiescence(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal: %v", err)
	}

	entered := make(chan struct{})
	releaseHost := make(chan struct{})
	closePublished := make(chan struct{})
	var writer *Instance
	rt.hooks.beforeClose = append(rt.hooks.beforeClose, func(ctx *InstanceContext) {
		if ctx.Instance != writer {
			return
		}
		if got := writer.invocationState.Load(); got&instanceInvocationClosed == 0 {
			t.Errorf("BeforeClose observed invocation state %#x without the close gate", got)
		}
		if _, err := writer.Invoke("write_after_host"); err == nil {
			t.Error("post-gate invocation entered before the final snapshot")
		}
		close(closePublished)
	})

	writerCode := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(import "env" "block" (func $block))
		(import "env" "table" (table 1 1 funcref))
		(import "env" "global" (global $global (mut funcref)))
		(func $target (type $target) (result i32) (i32.const 77))
		(elem declare func $target)
		(func (export "write_after_host")
			(call $block)
			(i32.const 0) (ref.func $target) (table.set 0)
			(ref.func $target) (global.set $global)))`)
	writer, err = rt.Instantiate(context.Background(), writerCode, WithImports(Imports{
		"env.block": HostFunc(func(HostModule, []uint64, []uint64) {
			close(entered)
			<-releaseHost
		}),
		"env.table":  table,
		"env.global": global,
	}))
	if err != nil {
		t.Fatalf("instantiate writer: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := writer.Invoke("write_after_host")
		callDone <- err
	}()
	<-entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- writer.Close() }()
	<-closePublished // exact barrier: no later invocation can enter
	close(releaseHost)
	if err := <-callDone; err == nil {
		t.Fatal("host-parked invocation completed without the close interruption")
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("writer Close: %v", err)
	}
	if writer.resourcesClosed || writer.resourceRefs != 2 {
		t.Fatalf("writer finalization state: resourcesClosed=%v roots=%d, want false/2", writer.resourcesClosed, writer.resourceRefs)
	}

	tableReaderCode := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(import "env" "table" (table 1 1 funcref))
		(func (export "call") (result i32)
			(i32.const 0) (call_indirect (type $target))))`)
	tableReader, err := rt.Instantiate(context.Background(), tableReaderCode, WithImports(Imports{"env.table": table}))
	if err != nil {
		t.Fatalf("instantiate table reader: %v", err)
	}
	if got := tableTestCallI32(t, tableReader, "call"); got != 77 {
		t.Fatalf("table call after writer close = %d, want 77", got)
	}

	globalReaderCode := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(import "env" "global" (global $global (mut funcref)))
		(table 1 1 funcref)
		(func (export "call") (result i32)
			(i32.const 0) (global.get $global) (table.set 0)
			(i32.const 0) (call_indirect (type $target))))`)
	globalReader, err := rt.Instantiate(context.Background(), globalReaderCode, WithImports(Imports{"env.global": global}))
	if err != nil {
		t.Fatalf("instantiate global reader: %v", err)
	}
	if got := tableTestCallI32(t, globalReader, "call"); got != 77 {
		t.Fatalf("global call after writer close = %d, want 77", got)
	}

	if err := tableReader.Close(); err != nil {
		t.Fatalf("table reader Close: %v", err)
	}
	if err := globalReader.Close(); err != nil {
		t.Fatalf("global reader Close: %v", err)
	}
	startRelease := make(chan struct{})
	releaseDone := make(chan error, 2)
	go func() { <-startRelease; releaseDone <- table.Close() }()
	go func() { <-startRelease; releaseDone <- global.Close() }()
	close(startRelease)
	for range 2 {
		if err := <-releaseDone; err != nil {
			t.Fatalf("container Close: %v", err)
		}
	}
	if !writer.resourcesClosed || writer.resourceRefs != 0 {
		t.Fatalf("writer after both roots release: resourcesClosed=%v roots=%d, want true/0", writer.resourcesClosed, writer.resourceRefs)
	}
}

func TestClosedProducerFuncrefInSharedTableStaysCallable(t *testing.T) {
	tbl, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	setterMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set0", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(123))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefFunc(0), []byte{0x26, 0x00})),
		)),
	)
	callerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)

	setter := tableTestInstantiateWithImports(t, setterMod, Imports{"env.t": tbl})
	caller := tableTestInstantiateWithImports(t, callerMod, Imports{"env.t": tbl})
	defer caller.Close()

	if _, err := setter.Invoke("set0"); err != nil {
		t.Fatalf("set0: %v", err)
	}

	// Close the producer before the reader calls. The table still holds the
	// producer's local funcref, so the producer must remain a retained root.
	if err := setter.Close(); err != nil {
		t.Fatalf("setter Close: %v", err)
	}
	if !setter.hasResourceRoots() {
		t.Fatal("producer not retained by shared table after close; its funcref would dangle")
	}
	if got := tableTestCallI32(t, caller, "callAt", I32(0)); got != 123 {
		t.Fatalf("callAt(0) after producer close = %d, want 123 (producer retained)", got)
	}
}

// A global.get element initializer copies the descriptor owned by the imported
// global's producer, not one represented in the writer's own descriptor arena.
// Closing the writer must transfer that actual producer to the persistent table
// before detaching the global import.
func TestGlobalGetElementRetainsActualProducerInSharedTable(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()

	producerCode := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(func $target (type $target) (result i32) (i32.const 42))
		(global (export "g") funcref (ref.func $target))
		(elem declare func $target))`)
	producer, err := rt.Instantiate(context.Background(), producerCode)
	if err != nil {
		t.Fatal(err)
	}
	global, err := producer.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}

	writerCode := mustCompileWat(rt, t, `(module
		(import "env" "g" (global funcref))
		(import "env" "t" (table 1 funcref))
		(elem (table 0) (i32.const 0) funcref (global.get 0)))`)
	writer, err := rt.Instantiate(context.Background(), writerCode, WithImports(Imports{"env.g": global, "env.t": table}))
	if err != nil {
		t.Fatal(err)
	}

	readerCode := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(import "env" "t" (table 1 funcref))
		(func (export "call") (result i32)
			(i32.const 0)
			(call_indirect (type $target))))`)
	reader, err := rt.Instantiate(context.Background(), readerCode, WithImports(Imports{"env.t": table}))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !producer.hasPhysicalResources() {
		t.Fatal("global-derived table entry released its actual producer")
	}
	if got := tableTestCallI32(t, reader, "call"); got != 42 {
		t.Fatalf("call after producer and writer close = %d, want 42", got)
	}
}

// The single-slot analog for funcref globals: a producer that writes its local
// funcref into an imported mutable funcref global via global.set and then closes
// must be retained by the global's owner until the value is overwritten or the
// global closes.
func TestClosedProducerFuncrefInSharedGlobalIsRetained(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	g, err := rt.NewFuncRefGlobal(FuncRef{}, true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal: %v", err)
	}

	producer := mustCompileWat(rt, t, `(module
		(import "env" "g" (global (mut funcref)))
		(func $f)
		(elem declare func $f)
		(func (export "store") (global.set 0 (ref.func $f))))`)
	in, err := rt.Instantiate(context.Background(), producer, WithImports(Imports{"env.g": g}))
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	if _, err := in.Invoke("store"); err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := in.Close(); err != nil {
		t.Fatalf("producer Close: %v", err)
	}
	if !in.hasResourceRoots() {
		t.Fatal("producer not retained by funcref global after close; its descriptor would dangle")
	}

	// Closing the global (now that its only importer is gone) releases the root.
	if err := g.Close(); err != nil {
		t.Fatalf("global Close: %v", err)
	}
	if in.hasResourceRoots() {
		t.Fatal("producer root not released after global close")
	}
}
