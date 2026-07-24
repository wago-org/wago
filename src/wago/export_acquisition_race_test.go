//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"
)

func TestExportAcquisitionCloseLinearization(t *testing.T) {
	t.Run("close wins", func(t *testing.T) {
		rt, in, _ := newExportAcquisitionFixture(t)
		defer rt.Close()
		gatePublished := make(chan struct{})
		releaseClose := make(chan struct{})
		rt.hooks.beforeClose = append(rt.hooks.beforeClose, func(ctx *InstanceContext) {
			if ctx.Instance == in {
				close(gatePublished)
				<-releaseClose
			}
		})
		closeDone := make(chan error, 1)
		go func() { closeDone <- in.Close() }()
		<-gatePublished

		if _, err := in.ExportedFunc("f"); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("ExportedFunc after close gate error = %v, want closed", err)
		}
		if _, err := in.ExportedTable("table"); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("ExportedTable after close gate error = %v, want closed", err)
		}
		if _, err := in.ExportedGlobalObject("global"); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("ExportedGlobalObject after close gate error = %v, want closed", err)
		}
		if _, err := in.ExportedMemory("memory"); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("ExportedMemory after close gate error = %v, want closed", err)
		}
		if got := in.Memory(); got != nil {
			t.Fatalf("Memory after close gate = %p, want nil", got)
		}

		close(releaseClose)
		if err := <-closeDone; err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	t.Run("Instance.Memory binds owner without exporting", func(t *testing.T) {
		rt, in, _ := newExportAcquisitionFixture(t)
		defer rt.Close()
		memory := in.Memory()
		if memory == nil || len(memory.Bytes()) != 1<<16 {
			t.Fatal("Instance.Memory acquisition was not usable")
		}
		memoryImporter := mustCompileWat(rt, t, `(module (import "env" "memory" (memory 1 1)))`)
		if _, err := rt.Instantiate(context.Background(), memoryImporter, WithImports(Imports{"env.memory": memory})); err == nil || !strings.Contains(err.Error(), "not been exported") {
			t.Fatalf("unexported Instance.Memory import error = %v, want explicit export requirement", err)
		}

		gatePublished := make(chan struct{})
		releaseClose := make(chan struct{})
		rt.hooks.beforeClose = append(rt.hooks.beforeClose, func(ctx *InstanceContext) {
			if ctx.Instance == in {
				close(gatePublished)
				<-releaseClose
			}
		})
		closeDone := make(chan error, 1)
		go func() { closeDone <- in.Close() }()
		<-gatePublished
		if got := memory.Bytes(); got != nil {
			t.Fatalf("Instance.Memory Bytes after close gate length = %d, want nil", len(got))
		}
		close(releaseClose)
		if err := <-closeDone; err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := memory.Bytes(); got != nil {
			t.Fatalf("Instance.Memory Bytes after physical close length = %d, want nil", len(got))
		}
	})

	t.Run("acquisition wins then handles fail closed", func(t *testing.T) {
		rt, in, consumerCode := newExportAcquisitionFixture(t)
		defer rt.Close()

		// This outer lease is a deterministic barrier proving the acquisition side
		// linearizes before Close. Each public acquisition also takes its own lease.
		if err := in.beginInvocation(); err != nil {
			t.Fatalf("outer acquisition lease: %v", err)
		}
		fn, err := in.ExportedFunc("f")
		if err != nil {
			t.Fatalf("ExportedFunc: %v", err)
		}
		table, err := in.ExportedTable("table")
		if err != nil {
			t.Fatalf("ExportedTable: %v", err)
		}
		global, err := in.ExportedGlobalObject("global")
		if err != nil {
			t.Fatalf("ExportedGlobalObject: %v", err)
		}
		memory, err := in.ExportedMemory("memory")
		if err != nil {
			t.Fatalf("ExportedMemory: %v", err)
		}
		if table.Size() != 1 || global.Get() != 42 || len(memory.Bytes()) != 1<<16 {
			t.Fatal("acquired exports were not usable before close")
		}

		gatePublished := make(chan struct{})
		releaseClose := make(chan struct{})
		rt.hooks.beforeClose = append(rt.hooks.beforeClose, func(ctx *InstanceContext) {
			if ctx.Instance == in {
				close(gatePublished)
				<-releaseClose
			}
		})
		closeDone := make(chan error, 1)
		go func() { closeDone <- in.Close() }()
		<-gatePublished

		// Acquired object methods and function attachment must reject the close gate
		// without touching storage that finalization may soon unmap.
		if got := global.Get(); got != 0 {
			t.Fatalf("global Get after close gate = %d, want fail-closed zero", got)
		}
		if got := memory.Bytes(); got != nil {
			t.Fatalf("memory Bytes after close gate length = %d, want nil", len(got))
		}
		if _, err := rt.Instantiate(context.Background(), consumerCode, WithImports(Imports{"env.f": fn})); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("function-handle attachment after close gate error = %v, want closed", err)
		}

		in.endInvocation()
		close(releaseClose)
		if err := <-closeDone; err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := table.Size(); got != 0 {
			t.Fatalf("table Size after physical close = %d, want 0", got)
		}
		if got := global.Get(); got != 0 {
			t.Fatalf("global Get after physical close = %d, want 0", got)
		}
		if err := global.Set(9); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("global Set after physical close error = %v, want closed", err)
		}
		if got := memory.Bytes(); got != nil {
			t.Fatalf("memory Bytes after physical close length = %d, want nil", len(got))
		}
		if got := in.Memory(); got != nil {
			t.Fatalf("Instance.Memory after physical close = %p, want nil", got)
		}
	})
}

func newExportAcquisitionFixture(t *testing.T) (*Runtime, *Instance, *Module) {
	t.Helper()
	rt := NewRuntime()
	module := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(func $f (export "f") (type $target) (result i32) (i32.const 42))
		(table (export "table") 1 1 funcref)
		(elem (i32.const 0) func $f)
		(memory (export "memory") 1 1)
		(global (export "global") (mut i64) (i64.const 42)))`)
	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		rt.Close()
		t.Fatalf("Instantiate fixture: %v", err)
	}
	consumer := mustCompileWat(rt, t, `(module
		(import "env" "f" (func $f (result i32)))
		(func (export "call") (result i32) (call $f)))`)
	return rt, in, consumer
}
