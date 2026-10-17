//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMemoryAttachmentCleanupPreservesObserverFailure(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, observerPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, observerPanic), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				producer, err := Instantiate(prefinalizerTableMemoryCompile(t, false, artifact))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				memory, err := producer.ExportedMemory("memory")
				if err != nil {
					t.Fatal(err)
				}
				table, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				primary := errors.New("first observer failure")
				var consumer *Instance
				var secondary *Memory
				callbacks := 0
				producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("producer cleanup") })
				var result *Instance
				_, panicked := cleanupTestPanic(func() {
					result, err = Instantiate(prefinalizerTableMemoryCompile(t, true, artifact), InstantiateOptions{runtime: rt, Imports: NewImports().Memory("env", "memory", memory).Table("env", "table", table), afterCreate: func(in *Instance) error {
						consumer = in
						secondary, err = in.ExportedMemory("secondary")
						if err != nil {
							return err
						}
						in.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("consumer cleanup") })
						if err := producer.Close(); err != nil {
							return err
						}
						if observerPanic {
							panic(primary)
						}
						return primary
					}})
				})
				if panicked || result != nil || !errors.Is(err, primary) || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 {
					t.Fatal("later physical panic replaced the primary construction failure", panicked, result, err, callbacks)
				}
				if consumer == nil || secondary == nil || secondary.jobMemory() != nil {
					t.Fatal("observer failure lost its real owned secondary mapping")
				}
				for _, in := range []*Instance{producer, consumer} {
					if in.finalizing || !in.resourcesClosed || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("construction cleanup stranded native ownership")
					}
				}
			})
		}
	}
}

func TestMemoryAttachmentCleanupRuntimeRegistrationFailure(t *testing.T) {
	pc := importedMemoryCompile(t, false, 0, false, false, 0)
	cc := importedMemoryCompile(t, true, 0, false, false, 0)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	memory, err := producer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	nativeExecutionMu.Lock()
	locked := true
	defer func() {
		if locked {
			nativeExecutionMu.Unlock()
		}
	}()
	type outcome struct {
		instance *Instance
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		in, err := Instantiate(cc, InstantiateOptions{runtime: rt, Imports: NewImports().Memory("env", "memory", memory)})
		done <- outcome{in, err}
	}()
	importedMemoryWaitAttachments(t, memory, 1)
	if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 {
		t.Fatal("queued construction lost its pending actual root", err)
	}
	nativeExecutionMu.Unlock()
	locked = false
	select {
	case out := <-done:
		if out.instance != nil || out.err == nil || !strings.Contains(out.err.Error(), "runtime closed during instantiation") {
			t.Fatal("fixture did not fail before terminal receipt handoff", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registration rollback failed to release native lease")
	}
	state := memory.state.Load()
	state.mu.Lock()
	count := state.importerCount()
	state.mu.Unlock()
	if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || count != 0 {
		t.Fatal("failed Runtime registration lost builder-owned actual memory receipt")
	}
}

func TestMemoryAttachmentCleanupBuilderFirstPanic(t *testing.T) {
	pc := prefinalizerTableMemoryCompile(t, false, false)
	memoryProducer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer memoryProducer.Close()
	functionProducer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer functionProducer.Close()
	memory, err := memoryProducer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	export, err := functionProducer.ExportedFunc("safe")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMemory(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	fn := append(append(wasmtest.Name("env"), wasmtest.Name("function")...), 0, 0)
	firstImport := append(append(wasmtest.Name("env"), wasmtest.Name("first")...), 2, 0, 1)
	secondImport := append(append(wasmtest.Name("env"), wasmtest.Name("second")...), 2, 0, 2)
	cc, err := Compile(nil, wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(fn, firstImport, secondImport)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("forward", 0, 0)))))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	first, later := new(int), new(int)
	visited := [2]int{}
	memoryProducer.referenceLifetime().afterPhysicalRelease(func() { visited[0]++; panic(first) })
	functionProducer.referenceLifetime().afterPhysicalRelease(func() { visited[1]++; panic(later) })
	state := second.state.Load()
	state.mu.Lock() // fail the later linker only after both earlier real acquisitions
	locked := true
	defer func() {
		if locked {
			state.mu.Unlock()
		}
	}()
	type outcome struct {
		instance *Instance
		err      error
		value    any
		panicked bool
	}
	done := make(chan outcome, 1)
	go func() {
		var out outcome
		out.value, out.panicked = cleanupTestPanic(func() {
			out.instance, out.err = Instantiate(cc, NewImports().Function("env", "function", export).Memory("env", "first", memory).Memory("env", "second", second))
		})
		done <- out
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := memory.state.Load()
		s.mu.Lock()
		count := s.importerCount()
		s.mu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			state.mu.Unlock()
			locked = false
			t.Fatal("constructor did not reach its first actual memory acquisition")
		}
		time.Sleep(time.Millisecond)
	}
	for _, producer := range []*Instance{memoryProducer, functionProducer} {
		if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 {
			t.Fatal("earlier constructor class failed to retain actual producer", err)
		}
	}
	state.mu.Unlock()
	locked = false
	select {
	case out := <-done:
		if !out.panicked || out.value != first || out.instance != nil || visited != [2]int{1, 1} {
			t.Fatal("later deferred class replaced first cleanup panic or skipped release", out, visited)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("builder cleanup failed to finish after later link rejection")
	}
	for _, producer := range []*Instance{memoryProducer, functionProducer} {
		if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil {
			t.Fatal("builder failure stranded actual native producer")
		}
	}
}
