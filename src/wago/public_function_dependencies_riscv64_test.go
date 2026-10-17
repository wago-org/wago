//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ActualFunctionDependenciesCompilerGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") != "load" {
		t.Skip("compiler-free qualification only")
	}
	if c, err := Compile(nil, rv64FunctionDependencyConsumerSource(false, false)); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			c.Close()
		}
		t.Fatal("compiler-free receipt runner unexpectedly has a source compiler", err)
	}
}

func rv64FunctionDependencyParams(vector bool) []wasm.ValType {
	params := []wasm.ValType{wasm.I32, wasm.I64, wasm.F64}
	if vector {
		params = append(params, wasm.V128)
	}
	return params
}

func rv64FunctionDependencyConsumerSource(vector, relay bool, badInitializer ...bool) []byte {
	params := rv64FunctionDependencyParams(vector)
	imports := [][]byte{rv64VectorFunctionImport("first", 0)}
	exports := [][]byte{wasmtest.ExportEntry("forward", 0, 0)}
	if relay {
		return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(7, wasmtest.Vec(exports...)))
	}
	imports = append(imports, rv64VectorFunctionImport("second", 0))
	exports = append(exports, wasmtest.ExportEntry("call", 0, 2))
	body := []byte{}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x10, 1, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0})}
	if len(badInitializer) != 0 && badInitializer[0] {
		sections = append(sections, wasmtest.Section(5, []byte{1, 0, 1})) // one-page owned memory
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	if len(badInitializer) != 0 && badInitializer[0] {
		// Validation permits this active segment; construction must reject its
		// one-byte write at the exact end, after native import acquisition. Its
		// saved initErr rolls back the installed, still-pending terminal holder.
		sections = append(sections, wasmtest.Section(11, []byte{1, 0, 0x41, 0x80, 0x80, 4, 0x0b, 1, 0xdd}))
	}
	return wasmtest.Module(sections...)
}

func rv64FunctionDependencySharedProducerSource(vector bool) []byte {
	params := rv64FunctionDependencyParams(vector)
	var body []byte
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))),
		wasmtest.Section(2, wasmtest.Vec(rv64VectorMemoryImport())), wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func rv64FunctionDependencyTableConsumerSource() []byte {
	params := rv64FunctionDependencyParams(false)
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 1, 2)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))),
		wasmtest.Section(2, wasmtest.Vec(rv64VectorFunctionImport("first", 0), rv64VectorFunctionImport("second", 0), tableImport)),
		wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}))))
}

func rv64FunctionDependencyPublicFixture(t *testing.T, vector, artifact bool) (*Compiled, *Instance, *InstanceExport, *Compiled) {
	t.Helper()
	pc := rv64NativeTailCompile(t, fmt.Sprintf("function-dep-provider-v%t", vector), rv64PublicNativeIdentityModule(rv64FunctionDependencyParams(vector)), artifact)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close() })
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	cc := rv64NativeTailCompile(t, fmt.Sprintf("function-dep-consumer-v%t", vector), rv64FunctionDependencyConsumerSource(vector, false), artifact)
	return pc, producer, export, cc
}

func rv64FunctionDependencyTestCount(ownership *rv64FunctionDependencyOwnership) int {
	if ownership == nil {
		return 0
	}
	return ownership.functions.n
}

func rv64FunctionDependencyTestRoot(ownership *rv64FunctionDependencyOwnership, index int) functionDependencyRoot {
	rows := &ownership.functions
	rows.mu.Lock()
	defer rows.mu.Unlock()
	if index < len(rows.inline) {
		return rows.inline[index].root
	}
	return rows.extra[index-len(rows.inline)].root
}

func rv64FunctionDependencyTestComplete(ownership *rv64FunctionDependencyOwnership) bool {
	if ownership == nil {
		return false
	}
	return ownership.functions.complete()
}

func rv64FunctionDependencyPublicState(t *testing.T, in, producer *Instance, tableEdge ...bool) *rv64FunctionDependencyOwnership {
	t.Helper()
	wantOutgoing := uint32(1)
	if len(tableEdge) != 0 && tableEdge[0] {
		rows := rv64ExistingTableDependencyRows(in)
		if rows == nil || rows.rows.n != 1 || rows.rows.inline[0].root.producer != producer {
			t.Fatal("exact imported table storage receipt")
		}
		receipt := rows.rows.inline[0].root.receipt
		receipt.graph.mu.Lock()
		record, _ := receipt.graph.recordLocked(receipt.identity)
		valid := record != nil && record.state == dependencyTyped && record.kind == dependencyTableStorage
		receipt.graph.mu.Unlock()
		if !valid {
			t.Fatal("table handoff did not adopt its actual storage edge")
		}
		wantOutgoing++
	}
	o := rv64ExistingFunctionDependencyOwnership(in)
	if o == nil || !o.managed || rv64FunctionDependencyTestCount(o) != 1 {
		t.Fatal("exact deduplicated terminal function receipt")
	}
	root := rv64FunctionDependencyTestRoot(o, 0)
	if root.producer != producer || root.receipt.graph != rv64FunctionDependencyGraph() || o.member.graph.Load() != rv64FunctionDependencyGraph() || o.member.outgoing.Load() != wantOutgoing {
		t.Fatal("exact deduplicated terminal function receipt")
	}
	return o
}

func TestRV64PublicFunctionDependenciesSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, runtimeMode := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-runtime%t", artifact, vector, runtimeMode), func(t *testing.T) {
					pc, producer, export, cc := rv64FunctionDependencyPublicFixture(t, vector, artifact)
					var first, second *Instance
					im := NewImports().Function("env", "first", export).Function("env", "second", export)
					if runtimeMode {
						one, two := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig())), NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, one)
						defer rv64HostCertificateCloseRuntime(t, two)
						first = rv64NativeTokenInstantiate(t, one, cc, im)
						reexport, err := first.ExportedFunc("forward")
						if err != nil || reexport.inst != producer {
							t.Fatal("actual reexport owner", err)
						}
						relay := rv64NativeTailCompile(t, fmt.Sprintf("function-dep-relay-v%t", vector), rv64FunctionDependencyConsumerSource(vector, true), artifact)
						second = rv64NativeTokenInstantiate(t, two, relay, NewImports().Function("env", "first", reexport))
					} else {
						var err error
						first, err = Instantiate(cc, InstantiateOptions{Imports: im, afterCreate: func(in *Instance) error {
							if !in.constructionIsActive() {
								return errors.New("direct no-start handoff lacks construction lease")
							}
							rv64FunctionDependencyPublicState(t, in, producer)
							return nil
						}})
						if err != nil {
							t.Fatal(err)
						}
						second, err = Instantiate(cc, im)
						if err != nil {
							t.Fatal(err)
						}
						defer first.Close()
						defer second.Close()
					}
					for _, in := range []*Instance{first, second} {
						rv64FunctionDependencyPublicState(t, in, producer)
					}
					rv64FunctionDependencyPhysicalShadow(t, producer, 2)
					args := []uint64{0xdeadbeef80000001, 0xfedcba9876543210, 0x7ff0000000000001}
					want := slices.Clone(args)
					want[0] = uint64(uint32(want[0]))
					if vector {
						args = append(args, 0x80000000aabbccdd, 0x0123456789abcdef)
						want = append(want, args[3:]...)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					pc.Close()
					cc.Close()
					for _, in := range []*Instance{first, second} {
						name := "call"
						if in == second && runtimeMode {
							name = "forward"
						}
						got, err := in.Invoke(name, args...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("retired numeric transport", got, want, err)
						}
					}
					first.Close()
					rv64FunctionDependencyPhysicalShadow(t, producer, 1)
					if err := second.Close(); err != nil || producer.jm != nil || producer.nativeContext != 0 {
						t.Fatal("last exact receipt retirement", err)
					}
					for _, in := range []*Instance{producer, first, second} {
						o := rv64ExistingFunctionDependencyOwnership(in)
						if o == nil || !o.cleanupCompleted.Load() || o.member.active.Load() || !rv64FunctionDependencyTestComplete(o) {
							t.Fatal("terminal registry/root leak")
						}
					}
				})
			}
		}
	}
}

func TestRV64PublicFunctionDependenciesRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, failure := range []string{"link", "initializer", "observer", "panic", "cancel"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, failure), func(t *testing.T) {
				_, producer, export, cc := rv64FunctionDependencyPublicFixture(t, false, artifact)
				im := NewImports().Function("env", "first", export).Function("env", "second", export)
				var captured *Instance
				sentinel := errors.New("observer denial")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				opts := InstantiateOptions{Imports: im, afterCreate: func(in *Instance) error {
					captured = in
					rv64FunctionDependencyPublicState(t, in, producer)
					if failure == "panic" {
						panic(sentinel)
					}
					if failure == "cancel" {
						cancel()
						return nil
					}
					return sentinel
				}}
				if failure == "link" {
					opts.Imports = NewImports().Function("env", "first", export)
				}
				if failure == "initializer" {
					cc = rv64NativeTailCompile(t, "function-dep-initializer-vfalse", rv64FunctionDependencyConsumerSource(false, false, true), artifact)
				}
				if failure == "cancel" {
					opts.startContext = ctx
				}
				in, err := Instantiate(cc, opts)
				if in != nil || err == nil {
					t.Fatal("failed constructor published", err)
				}
				if failure == "initializer" && (captured != nil || !strings.Contains(err.Error(), "active data segment 0 out of bounds on memory 0")) {
					t.Fatal("pending terminal-holder initializer rollback lost its exact cause", err)
				}
				if failure == "link" {
					// Preflight rejects missing imports before any builder acquisition.
					if producer.importState.Load() != nil || captured != nil || dependencyTestRefs(producer) != 0 {
						t.Fatal("preflight failure acquired a root or published a holder")
					}
				} else {
					rv64FunctionDependencyPhysicalShadow(t, producer, 0)
				}
				if captured != nil {
					o := rv64ExistingFunctionDependencyOwnership(captured)
					if !o.managed || !o.cleanupCompleted.Load() || o.member.active.Load() || !rv64FunctionDependencyTestComplete(o) || captured.jm != nil {
						t.Fatal("posthandoff rollback owner")
					}
					if !railshotDrainInstanceFunctionDependencies(captured) {
						t.Fatal("drained holder fell back to rescan")
					}
				}
			})
		}
	}
}

func TestRV64PublicFunctionDependenciesPhysicalCompletion(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, path := range []string{"typed", "anonymous", "consumer"} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-panic%t", artifact, path, panics), func(t *testing.T) {
					_, producer, export, cc := rv64FunctionDependencyPublicFixture(t, false, artifact)
					consumer, err := Instantiate(cc, NewImports().Function("env", "first", export).Function("env", "second", export))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					rv64FunctionDependencyPublicState(t, consumer, producer)
					owner := producer
					var release func() error
					if path == "consumer" {
						owner = consumer
						release = consumer.Close
					} else if path == "typed" {
						producer.Close()
						release = consumer.Close
					} else {
						if !producer.retainResourceRoot() {
							t.Fatal("anonymous root")
						}
						consumer.Close()
						producer.Close()
						release = func() error { producer.releaseResourceRoot(); return nil }
					}
					entered, resume := make(chan struct{}), make(chan struct{})
					marker := new(int)
					owner.referenceLifetime().afterPhysicalRelease(func() {
						close(entered)
						<-resume
						if panics {
							panic(marker)
						}
					})
					type outcome struct {
						err      error
						value    any
						panicked bool
					}
					done := make(chan outcome, 1)
					go func() {
						var result outcome
						returned := false
						defer func() {
							if !returned {
								result.value = recover()
								result.panicked = true
							}
							done <- result
						}()
						result.err = release()
						returned = true
					}()
					select {
					case <-entered:
					case <-time.After(5 * time.Second):
						close(resume)
						<-done
						t.Fatal("physical callback never entered")
					}
					o := rv64ExistingFunctionDependencyOwnership(owner)
					if o.cleanupCompleted.Load() || !o.member.active.Load() || owner.jm != nil || owner.nativeContext != 0 {
						close(resume)
						<-done
						t.Fatal("retirement preceded full physical callback completion")
					}
					close(resume)
					result := <-done
					if !panics && (result.err != nil || result.panicked) || panics && path == "anonymous" && (!result.panicked || result.value != marker || result.err != nil) || panics && path != "anonymous" && (result.panicked || !errors.Is(result.err, ErrCallbackPanic)) {
						t.Fatal("physical callback exact outcome", path, panics, result)
					}
					if !o.cleanupCompleted.Load() || o.member.active.Load() {
						t.Fatal("postcallback registration leak")
					}
					producer.Close()
					for _, in := range []*Instance{producer, consumer} {
						state := rv64ExistingFunctionDependencyOwnership(in)
						if state == nil || !state.cleanupCompleted.Load() || state.member.active.Load() || state.member.outgoing.Load() != 0 || !rv64FunctionDependencyTestComplete(state) || !in.resourcesClosed || in.jm != nil || in.ar != nil || in.eng != nil || in.nativeContext != 0 || in.base != 0 {
							t.Fatal("callback panic interrupted producer/consumer native teardown", path)
						}
					}
				})
			}
		}
	}
}

func TestRV64PublicFunctionDependenciesRegistrationCloseRace(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			_, producer, _, _ := rv64FunctionDependencyPublicFixture(t, false, artifact)
			g, err := newRV64FunctionDependencyGraph(1, 32)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var workers sync.WaitGroup
			workers.Add(25)
			failures := make(chan error, 24)
			for range 24 {
				go func() {
					defer workers.Done()
					<-start
					var batch functionImportAttachments
					err := rv64AcquireFunctionDependency(&batch, producer, g)
					if err == nil {
						batch.detachAll()
					} else if !errors.Is(err, errDependencyClosed) && !errors.Is(err, errDependencyMember) {
						failures <- err
					}
				}()
			}
			go func() { defer workers.Done(); <-start; _ = producer.Close() }()
			close(start)
			workers.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			g.mu.Lock()
			nodes, receipts := len(g.owners), g.live
			g.mu.Unlock()
			if nodes != 0 || receipts != 0 || producer.jm != nil || dependencyTestRefs(producer) != 0 {
				t.Fatal("registration/retain/Close stranded node", nodes, receipts)
			}
		})
	}
}

func TestRV64PublicFunctionDependenciesSameHome(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-panic%t", artifact, vector, panics), func(t *testing.T) {
					memory, err := NewSharedMemory(1, 4)
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					pc := rv64NativeTailCompile(t, fmt.Sprintf("function-dep-same-home-provider-v%t", vector), rv64FunctionDependencySharedProducerSource(vector), artifact)
					var producers [2]*Instance
					im := NewImports()
					for i := range producers {
						producers[i], err = Instantiate(pc, NewImports().Memory("env", "memory", memory))
						if err != nil {
							t.Fatal(err)
						}
						defer producers[i].Close()
						export, err := producers[i].ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						im.Function("env", []string{"first", "second"}[i], export)
					}
					if producers[0] == producers[1] || producers[0].jm != producers[1].jm || producers[0].nativeContext == producers[1].nativeContext {
						t.Fatal("distinct instance identities must share only JobMemory")
					}
					cc := rv64NativeTailCompile(t, fmt.Sprintf("function-dep-consumer-v%t", vector), rv64FunctionDependencyConsumerSource(vector, false), artifact)
					consumer, err := Instantiate(cc, im)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					owner := rv64ExistingFunctionDependencyOwnership(consumer)
					if owner == nil || !owner.managed || rv64FunctionDependencyTestCount(owner) != 2 || owner.member.outgoing.Load() != 2 {
						t.Fatal("same home collapsed concrete producer receipts")
					}
					for i, producer := range producers {
						root := rv64FunctionDependencyTestRoot(owner, i)
						if root.producer != producer || root.receipt.graph != rv64FunctionDependencyGraph() {
							t.Fatal("receipt producer identity")
						}
						rv64FunctionDependencyPhysicalShadow(t, producer, 1)
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
					}
					args, want := rv64NativeVectorInputs(rv64FunctionDependencyParams(vector))
					for _, name := range []string{"forward", "call"} {
						got, err := consumer.Invoke(name, args...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("same-home retired producer transport", name, got, err)
						}
					}
					callbacks := [2]int{}
					for i, producer := range producers {
						producer.referenceLifetime().afterPhysicalRelease(func() {
							callbacks[i]++
							if i == 0 && panics {
								panic("first producer callback")
							}
						})
					}
					closeErr := consumer.Close()
					if panics && !errors.Is(closeErr, ErrCallbackPanic) || !panics && closeErr != nil || callbacks != [2]int{1, 1} {
						t.Fatal("all exact acquisitions must drain through first callback panic", closeErr, callbacks)
					}
					for _, in := range []*Instance{producers[0], producers[1], consumer} {
						state := rv64ExistingFunctionDependencyOwnership(in)
						if state == nil || !state.cleanupCompleted.Load() || state.member.active.Load() || state.member.outgoing.Load() != 0 || !rv64FunctionDependencyTestComplete(state) || in.jm != nil || in.nativeContext != 0 {
							t.Fatal("same-home or panic cleanup leak")
						}
					}
				})
			}
		}
	}
}

func TestRV64PublicFunctionDependenciesImportedTableHandoff(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, failure := range []string{"none", "observer", "panic"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, failure), func(t *testing.T) {
				params := rv64FunctionDependencyParams(false)
				pc := rv64NativeTailCompile(t, "function-dep-table-provider", rv64ForeignTableCallModule(params, 9, false, false), artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("target9")
				if err != nil {
					t.Fatal(err)
				}
				table, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				cc := rv64NativeTailCompile(t, "function-dep-table-consumer", rv64FunctionDependencyTableConsumerSource(), artifact)
				if !cc.executionView().nativeTableContextABI || cc.executionView().tableImportCount() != 1 {
					t.Fatal("fixture must exercise imported-table publication lease")
				}
				before := dependencyTestRefs(producer)
				var captured *Instance
				sentinel := errors.New("post-table-handoff observer denial")
				consumer, err := Instantiate(cc, InstantiateOptions{Imports: NewImports().Function("env", "first", export).Function("env", "second", export).Table("env", "table", table), afterCreate: func(in *Instance) error {
					captured = in
					if !in.constructionIsActive() || !nativeExecutionMu.TryLock() {
						return errors.New("receipt adoption preceded native lease release or lacked construction ownership")
					}
					nativeExecutionMu.Unlock()
					rv64FunctionDependencyPublicState(t, in, producer, true)
					if failure == "panic" {
						panic(sentinel)
					}
					if failure == "observer" {
						return sentinel
					}
					return nil
				}})
				if captured == nil {
					t.Fatal("constructor did not reach concrete table handoff", err)
				}
				if failure == "none" {
					if consumer != captured || err != nil {
						t.Fatal("successful table handoff", err)
					}
					defer consumer.Close()
					args, want := rv64NativeVectorInputs(params)
					want[0] = uint64(uint32(want[0] + 9))
					if got, err := consumer.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
						t.Fatal("post-handoff native call", got, err)
					}
					if err := consumer.Close(); err != nil {
						t.Fatal(err)
					}
				} else if consumer != nil || !errors.Is(err, sentinel) {
					t.Fatal("table handoff failure published or lost cause", err)
				}
				// The initialized row is reachable through the consumer's native
				// imports, so existing table retirement retains that closed consumer.
				// Removing the actual row through public guest execution must finish
				// its installed holder. C5aj does not collect reference cycles.
				pending := rv64ExistingFunctionDependencyOwnership(captured)
				if pending == nil || rv64FunctionDependencyTestCount(pending) != 1 || pending.cleanupCompleted.Load() {
					t.Fatal("table retention lost terminal function ownership")
				}
				if got, err := producer.Invoke("clear", 0); err != nil || !slices.Equal(got, []uint64{9}) {
					t.Fatal("public table row removal", got, err)
				}
				state := rv64ExistingFunctionDependencyOwnership(captured)
				if state == nil || !state.managed || !state.cleanupCompleted.Load() || state.member.active.Load() || !rv64FunctionDependencyTestComplete(state) || captured.jm != nil || dependencyTestRefs(producer) != before {
					t.Fatalf("imported table terminal rollback: state=%+v jm=%p producer roots=%d before=%d", state, captured.jm, dependencyTestRefs(producer), before)
				}
			})
		}
	}
}
