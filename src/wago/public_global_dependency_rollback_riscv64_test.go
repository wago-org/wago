//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64WaitGlobalImporters(t *testing.T, global *Global, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		global.owner.mu.Lock()
		count := global.owner.importers
		global.owner.mu.Unlock()
		if count == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("constructor did not reach its actual global acquisition", count, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRV64PublicGlobalDependenciesRuntimeRegistrationFailure(t *testing.T) {
	pc := rv64NativeTailCompile(t, "global-dep-provider-vfalse-mtrue", rv64GlobalDependencyModule(false, false, false, true), false)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	first, err := producer.ExportedGlobalObject("g0")
	if err != nil {
		t.Fatal(err)
	}
	second, err := producer.ExportedGlobalObject("g1")
	if err != nil {
		t.Fatal(err)
	}
	memory, err := producer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	cc := rv64NativeTailCompile(t, "global-dep-consumer-vfalse-ffalse-mtrue", rv64GlobalDependencyModule(true, false, false, true), false)
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
		in, err := Instantiate(cc, InstantiateOptions{runtime: rt, Imports: NewImports().Global("env", "first", first).Global("env", "second", second).Memory("env", "memory", memory)})
		done <- outcome{in, err}
	}()
	rv64WaitGlobalImporters(t, first, 1)
	rv64WaitGlobalImporters(t, second, 1)
	importedMemoryWaitAttachments(t, memory, 1)
	if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 3 {
		t.Fatal("queued registration lost actual global and memory roots", err)
	}
	nativeExecutionMu.Unlock()
	locked = false
	select {
	case out := <-done:
		if out.instance != nil || out.err == nil || !strings.Contains(out.err.Error(), "runtime closed during instantiation") {
			t.Fatal("fixture did not fail before terminal handoff", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registration rollback did not release the native construction lease")
	}
	rv64CheckGlobalDependencyRetirement(t, producer)
	if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || first.owner.importers != 0 || second.owner.importers != 0 || memory.state.Load().importerCount() != 0 {
		t.Fatal("failed registration stranded builder-owned global or memory storage")
	}
}

func TestRV64PublicGlobalDependenciesBuilderFirstPanic(t *testing.T) {
	pc := importedGlobalCompile(t, false, 0, true)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	first, err := producer.ExportedGlobalObject("g0")
	if err != nil {
		t.Fatal(err)
	}
	second := NewGlobalI32(7, true)
	defer second.Close()
	// Only this later count is synthetic. The first cell, producer retention,
	// receipt and native physical cleanup are actual constructor operations.
	second.owner.importers = maxInt()
	defer func() { second.owner.importers = 0 }()
	cc := rv64NativeTailCompile(t, "global-dep-consumer-vfalse-ffalse-mfalse", rv64GlobalDependencyModule(true, false, false, false), false)
	marker := new(int)
	visited := 0
	producer.referenceLifetime().afterPhysicalRelease(func() { visited++; panic(marker) })
	graph := rv64FunctionDependencyGraph()
	graph.mu.Lock()
	graphLocked := true
	defer func() {
		if graphLocked {
			graph.mu.Unlock()
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
			out.instance, out.err = Instantiate(cc, NewImports().Global("env", "first", first).Global("env", "second", second))
		})
		done <- out
	}()
	deadline := time.Now().Add(3 * time.Second)
	for rv64ExistingFunctionDependencyOwnership(producer) == nil {
		if time.Now().After(deadline) {
			t.Fatal("constructor did not finish its initial global snapshot")
		}
		time.Sleep(time.Millisecond)
	}
	first.owner.mu.Lock()
	ownerLocked := true
	defer func() {
		if ownerLocked {
			first.owner.mu.Unlock()
		}
	}()
	graph.mu.Unlock()
	graphLocked = false
	for {
		producer.lifeMu.Lock()
		retained := producer.resourceRefs == 1
		producer.lifeMu.Unlock()
		if retained {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("constructor did not retain the actual producer")
		}
		time.Sleep(time.Millisecond)
	}
	if err := producer.Close(); err != nil || producer.resourcesClosed {
		t.Fatal("pending actual receipt did not pin producer", err)
	}
	first.owner.mu.Unlock()
	ownerLocked = false
	select {
	case out := <-done:
		if !out.panicked || out.value != marker || out.instance != nil || visited != 1 {
			t.Fatal("builder rollback lost its first physical panic", out, visited)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("builder rollback did not finish")
	}
	rv64CheckGlobalDependencyRetirement(t, producer)
	if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || first.owner.importers != 0 || second.owner.importers != maxInt() {
		t.Fatal("builder rollback lost a successful count or consumed a denied count")
	}
}

func TestRV64PublicGlobalDependenciesStartFailure(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "artifactfalse", true: "artifacttrue"}[artifact], func(t *testing.T) {
			pc := rv64NativeTailCompile(t, "global-dep-provider-vfalse-mfalse", rv64GlobalDependencyModule(false, false, false, false), artifact)
			producer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			first, err := producer.ExportedGlobalObject("g0")
			if err != nil {
				t.Fatal(err)
			}
			second, err := producer.ExportedGlobalObject("g1")
			if err != nil {
				t.Fatal(err)
			}
			source := rv64GlobalDependencyModule(true, false, false, false)
			source = rv64ReplaceTableStageSection(t, source, 1, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}), wasmtest.FuncType(nil, nil))))
			source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{4, 1, 2, 0, 3}))
			source = rv64ReplaceTableStageSection(t, source, 10, append(wasmtest.Section(8, []byte{3}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x23, 1, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x23, 0, 0x23, 1, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0, 0x0b})))...))
			cc := rv64NativeTailCompile(t, "global-dep-start-failure-consumer", source, artifact)
			var consumer *Instance
			callbacks := 0
			producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("producer start cleanup") })
			result, err := Instantiate(cc, InstantiateOptions{Imports: NewImports().Global("env", "first", first).Global("env", "second", second), afterCreate: func(in *Instance) error {
				consumer = in
				if err := rv64CheckGlobalDependencyAdoption(in, producer, 2); err != nil {
					return err
				}
				in.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("consumer start cleanup") })
				return producer.Close()
			}})
			var trap *TrapError
			if result != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 || consumer == nil || !rv64ExistingGlobalDependencyRows(consumer).complete() {
				t.Fatal("actual start trap was replaced or global cleanup skipped", err, callbacks)
			}
			for _, in := range []*Instance{producer, consumer} {
				rv64CheckGlobalDependencyRetirement(t, in)
				if !in.resourcesClosed || in.finalizing || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil || in.nativeContext != 0 {
					t.Fatal("failed start stranded actual native ownership")
				}
			}
			if first.owner.importers != 0 || second.owner.importers != 0 {
				t.Fatal("failed start stranded counted global imports")
			}
		})
	}
}

// Validate only the exact members created by this fixture. Other process graph
// owners may still be live; neither graph.live nor a registry reset proves this.
func rv64CheckGlobalDependencyRetirement(t *testing.T, in *Instance) {
	t.Helper()
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || !ownership.cleanupCompleted.Load() || ownership.member.active.Load() || ownership.member.outgoing.Load() != 0 {
		t.Fatal("physical completion left its actual dependency member active")
	}
	member := &ownership.member
	graph := member.graph.Load()
	if graph == nil || member.identity == 0 {
		t.Fatal("retirement fixture did not register its actual member")
	}
	graph.mu.Lock()
	absent := graph.owners[in] == nil && member.receipts == 0
	graph.mu.Unlock()
	if !absent {
		t.Fatal("physical completion left its exact member in the registry")
	}
}
