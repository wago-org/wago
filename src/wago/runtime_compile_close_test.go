package wago

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func waitRuntimeClosed(t *testing.T, rt *Runtime) {
	t.Helper()
	for {
		rt.mu.Lock()
		closed := rt.closed
		rt.mu.Unlock()
		if closed {
			return
		}
		runtime.Gosched()
	}
}

func TestRuntimeCompileRejectsClosedRuntimeBeforeHooks(t *testing.T) {
	rt := NewRuntime()
	var beforeCalls int
	rt.hooks.BeforeCompile(func(*CompileContext, []byte) ([]byte, error) {
		beforeCalls++
		return nil, nil
	})
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Compile(wasmtest.Module()); err == nil || !strings.Contains(err.Error(), "closed runtime") {
		t.Fatalf("Compile after Close = %v, want closed-runtime error", err)
	}
	if beforeCalls != 0 {
		t.Fatalf("BeforeCompile ran %d times after close", beforeCalls)
	}
}

func TestRuntimeCloseWaitsForAdmittedCompileBeforePluginTeardown(t *testing.T) {
	rt := NewRuntime()
	entered := make(chan struct{})
	release := make(chan struct{})
	afterCompile := make(chan struct{})
	stopped := make(chan struct{})
	rt.hooks.BeforeCompile(func(*CompileContext, []byte) ([]byte, error) {
		close(entered)
		<-release
		return nil, nil
	})
	rt.hooks.AfterCompile(func(*CompileContext, *Module) error {
		close(afterCompile)
		return nil
	})
	rt.pluginStops = []registeredPluginStop{{name: "compiler", stop: func(context.Context) error {
		select {
		case <-afterCompile:
		default:
			t.Error("plugin stopped before admitted compile finished its AfterCompile hook")
		}
		close(stopped)
		return nil
	}}}

	compileDone := make(chan error, 1)
	go func() {
		_, err := rt.Compile(wasmtest.Module())
		compileDone <- err
	}()
	<-entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- rt.Close() }()
	waitRuntimeClosed(t, rt)
	select {
	case <-stopped:
		t.Fatal("plugin stopped while an admitted compile was still active")
	default:
	}
	close(release)
	if err := <-compileDone; err != nil {
		t.Fatalf("compile admitted before Close = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCompileUsesOneAdmissionSnapshot(t *testing.T) {
	rt := NewRuntime()
	entered := make(chan struct{})
	release := make(chan struct{})
	rt.hooks.BeforeCompile(func(*CompileContext, []byte) ([]byte, error) {
		close(entered)
		<-release
		return nil, nil
	})

	moduleDone := make(chan *Module, 1)
	errDone := make(chan error, 1)
	go func() {
		mod, err := rt.Compile(returningImportModule(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			[]byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b},
		))
		moduleDone <- mod
		errDone <- err
	}()
	<-entered
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatal(err)
	}
	close(release)
	mod := <-moduleDone
	if err := <-errDone; err != nil {
		t.Fatal(err)
	}
	imports := mod.Imports()
	if len(imports) != 1 || imports[0].Provided || imports[0].HasCapability {
		t.Fatalf("admitted compile observed later import generation: %+v", imports)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCompileCloseRaceHasOnlyAdmittedOrClosedOutcomes(t *testing.T) {
	for i := 0; i < 100; i++ {
		rt := NewRuntime()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var compileErr, closeErr error
		go func() {
			defer wg.Done()
			<-start
			_, compileErr = rt.Compile(wasmtest.Module())
		}()
		go func() {
			defer wg.Done()
			<-start
			closeErr = rt.Close()
		}()
		close(start)
		wg.Wait()
		if closeErr != nil {
			t.Fatalf("iteration %d Close = %v", i, closeErr)
		}
		if compileErr != nil && !strings.Contains(compileErr.Error(), "closed runtime") {
			t.Fatalf("iteration %d Compile = %v", i, compileErr)
		}
	}
}
