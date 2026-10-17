//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MemoryDependencyModule(imported, wide, function bool) []byte {
	addr := rv64M64Addr(wide)
	funcs := []rv64MMFunc{
		{"load", []wasm.ValType{addr}, []wasm.ValType{wasm.I32}, append(append([]byte{0x20, 0}, rv64M64Arg([]byte{0x28}, 0, 0)...), 0x0b)},
		{"store", []wasm.ValType{addr, wasm.I32}, nil, append(append([]byte{0x20, 0, 0x20, 1}, rv64M64Arg([]byte{0x36}, 0, 0)...), 0x0b)},
		{"grow", []wasm.ValType{addr}, []wasm.ValType{addr}, []byte{0x20, 0, 0x40, 0, 0x0b}},
		{"size", nil, []wasm.ValType{addr}, []byte{0x3f, 0, 0x0b}},
		{"identity", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x0b}},
	}
	var types, definitions, exports, bodies, imports [][]byte
	shift := uint32(0)
	if imported && function {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name("function")...), 0, 4)
		imports = append(imports, entry)
		shift = 1
	}
	for i, fn := range funcs {
		types = append(types, wasmtest.FuncType(fn.params, fn.results))
		definitions = append(definitions, wasmtest.ULEB(uint32(i)))
		index := uint32(i) + shift
		if imported && function && fn.name == "identity" {
			index = 0 // canonical native reexport preserves the actual producer
		}
		exports = append(exports, wasmtest.ExportEntry(fn.name, 0, index))
		bodies = append(bodies, wasmtest.Code(fn.body))
	}
	if imported {
		imports = append(imports, rv64M64Import("first", wide, 1, 4), rv64M64Import("second", wide, 1, 4))
	}
	exports = append(exports, wasmtest.ExportEntry("m0", 2, 0), wasmtest.ExportEntry("m1", 2, 1))
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(definitions...)))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(rv64M64Limit(wide, 1, 4), rv64M64Limit(wide, 1, 4))))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))...)
}

func TestRV64PublicMemoryDependenciesSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				for _, function := range []bool{false, true} {
					for _, runtimeMode := range []bool{false, true} {
						t.Run(fmt.Sprintf("artifact%t-wide%t-alias%t-function%t-runtime%t", artifact, wide, alias, function, runtimeMode), func(t *testing.T) {
							pc := rv64NativeTailCompile(t, fmt.Sprintf("memory-dep-provider-wide%t", wide), rv64MemoryDependencyModule(false, wide, false), artifact)
							producer, err := Instantiate(pc)
							if err != nil {
								t.Fatal(err)
							}
							defer producer.Close()
							memory, err := producer.ExportedMemory("m0")
							if err != nil {
								t.Fatal(err)
							}
							second, err := producer.ExportedMemory("m1")
							if err != nil || second == memory {
								t.Fatal("distinct exported handles", err)
							}
							if alias {
								second = memory
							}
							cc := rv64NativeTailCompile(t, fmt.Sprintf("memory-dep-consumer-wide%t-function%t", wide, function), rv64MemoryDependencyModule(true, wide, function), artifact)
							imports := NewImports().Memory("env", "first", memory).Memory("env", "second", second)
							if function {
								export, err := producer.ExportedFunc("identity")
								if err != nil {
									t.Fatal(err)
								}
								imports.Function("env", "function", export)
							}
							var consumers [2]*Instance
							for i := range consumers {
								if i == 1 && runtimeMode {
									forwarded, err := consumers[0].ExportedMemory("m0")
									other, otherErr := consumers[0].ExportedMemory("m1")
									if err != nil || otherErr != nil || forwarded != memory || other != second || forwarded.instanceOwner() != producer {
										t.Fatal("cross-Runtime reexport changed actual memory owner", err, otherErr)
									}
									imports = NewImports().Memory("env", "first", forwarded).Memory("env", "second", other)
									if function {
										export, err := consumers[0].ExportedFunc("identity")
										if err != nil || export.inst != producer {
											t.Fatal("canonical function reexport changed actual producer", err)
										}
										imports.Function("env", "function", export)
									}
								}
								if runtimeMode {
									rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
									defer rv64HostCertificateCloseRuntime(t, rt)
									consumers[i] = rv64NativeTokenInstantiate(t, rt, cc, imports)
								} else {
									consumers[i], err = Instantiate(cc, InstantiateOptions{Imports: imports, afterCreate: func(in *Instance) error {
										if !in.constructionIsActive() || !nativeExecutionMu.TryLock() {
											return fmt.Errorf("memory handoff lacks construction exclusion or still owns native lease")
										}
										nativeExecutionMu.Unlock()
										return nil
									}})
									if err != nil {
										t.Fatal(err)
									}
								}
								defer consumers[i].Close()
								want := 2
								if alias {
									want = 1
								}
								rows := rv64ExistingMemoryDependencyRows(consumers[i])
								ownership := rv64ExistingFunctionDependencyOwnership(consumers[i])
								outgoing := want
								if function {
									outgoing++
								}
								if rows == nil || rows.rows.n != want || int(ownership.member.outgoing.Load()) != outgoing {
									t.Fatal("actual per-handle rows/function receipt are not distinct")
								}
							}
							if err := producer.Close(); err != nil || producer.resourcesClosed {
								t.Fatal("actual receipts did not retain closed producer", err)
							}
							address, delta := uint64(16), uint64(1)
							if !wide {
								address |= 0xdeadbeef00000000
								delta |= 0xdeadbeef00000000
							}
							if _, err := consumers[0].Invoke("store", address, 0xdeadbeef80000001); err != nil {
								t.Fatal(err)
							}
							rv64MMCall(t, consumers[1], "load", []uint64{0x80000001}, address)
							rv64MMCall(t, consumers[0], "grow", []uint64{1}, delta)
							rv64MMCall(t, consumers[1], "size", []uint64{2})
							if function {
								rv64MMCall(t, consumers[1], "identity", []uint64{0x80000001}, 0xdeadbeef80000001)
							}
							for i, consumer := range consumers {
								if err := consumer.Close(); err != nil || !rv64ExistingMemoryDependencyRows(consumer).complete() {
									t.Fatal("exact managed counter drain", err)
								}
								if i == 0 && producer.resourcesClosed {
									t.Fatal("first consumer prematurely released actual producer")
								}
							}
							if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.eng != nil || memory.jobMemory() != nil || second.jobMemory() != nil {
								t.Fatal("last memory consumer did not release actual producer storage")
							}
						})
					}
				}
			}
		}
	}
}

func TestRV64PublicMemoryDependenciesSWAR(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			pc := rv64NativeTailCompile(t, "memory-dep-swar-provider", rv64PublicSWARMemoryModule(false), artifact)
			cc := rv64NativeTailCompile(t, "memory-dep-swar-consumer", rv64PublicSWARMemoryModule(true), artifact)
			producer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			memory, err := producer.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := Instantiate(cc, NewImports().Memory("env", "memory", memory))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			rows := rv64ExistingMemoryDependencyRows(consumer)
			if rows == nil || rows.rows.n != 1 || rows.rows.inline[0].root.producer != producer || rv64ExistingFunctionDependencyOwnership(consumer).member.outgoing.Load() != 1 {
				t.Fatal("SWAR import did not acquire its actual memory receipt")
			}
			if err := producer.Close(); err != nil || producer.resourcesClosed {
				t.Fatal("SWAR producer was not physically retained", err)
			}
			lo, hi := uint64(0x7ff0000000000001), uint64(0x80000000deadbeef)
			if _, err := consumer.Invoke("store", 0xdeadbeef00000003, lo, hi); err != nil {
				t.Fatal(err)
			}
			rv64MMCall(t, consumer, "load", []uint64{lo, hi}, 0xdeadbeef00000003)
			rv64MMCall(t, consumer, "grow", []uint64{lo, hi, 1}, 0xdeadbeef00000001, lo, hi)
			rv64MMCall(t, consumer, "size", []uint64{2})
			if err := consumer.Close(); err != nil || !rows.complete() || !producer.resourcesClosed || producer.jm != nil || memory.jobMemory() != nil {
				t.Fatal("SWAR receipt did not complete actual native cleanup", err)
			}
		})
	}
}

func TestRV64PublicMemoryDependenciesSameHome(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			home, err := NewSharedMemory(1, 4)
			if err != nil {
				t.Fatal(err)
			}
			defer home.Close()
			pc := rv64NativeTailCompile(t, "memory-dep-same-home-provider", rv64MMModule([]rv64MMFunc{{"identity", []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x0b}}}, [][]byte{rv64M64Limit(false, 1, 4)}, [][]byte{rv64MMImport("home", 1, 4)}, 2, nil), artifact)
			var producers [2]*Instance
			var memories [2]*Memory
			for i := range producers {
				producers[i], err = Instantiate(pc, NewImports().Memory("env", "home", home))
				if err != nil {
					t.Fatal(err)
				}
				defer producers[i].Close()
				memories[i], err = producers[i].ExportedMemory("m1")
				if err != nil || memories[i].instanceOwner() != producers[i] {
					t.Fatal("secondary exported memory lost actual producer", err)
				}
			}
			if producers[0].jm != producers[1].jm || producers[0] == producers[1] {
				t.Fatal("fixture does not share JobMemory across distinct producers")
			}
			cc := rv64NativeTailCompile(t, "memory-dep-consumer-widefalse-functionfalse", rv64MemoryDependencyModule(true, false, false), artifact)
			consumer, err := Instantiate(cc, NewImports().Memory("env", "first", memories[0]).Memory("env", "second", memories[1]))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			rows := rv64ExistingMemoryDependencyRows(consumer)
			if rows.rows.n != 2 || rows.rows.inline[0].root.producer != producers[0] || rows.rows.inline[1].root.producer != producers[1] {
				t.Fatal("same-home producers were deduplicated by backing memory")
			}
			for _, producer := range producers {
				if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 {
					t.Fatal("distinct actual producer memory receipt lost", err)
				}
			}
			if _, err := consumer.Invoke("store", 0xdeadbeef00000000, 0xdeadbeef80000001); err != nil {
				t.Fatal(err)
			}
			rv64MMCall(t, consumer, "load", []uint64{0x80000001}, 0)
			if err := consumer.Close(); err != nil || !rows.complete() {
				t.Fatal(err)
			}
			for _, producer := range producers {
				if !producer.resourcesClosed || producer.jm != nil || producer.eng != nil || producer.resourceRefs != 0 {
					t.Fatal("same-home owner cleanup leak")
				}
			}
			if err := home.Close(); err != nil {
				t.Fatal("host-only importer rows remained attached", err)
			}
		})
	}
}

func TestRV64PublicMemoryDependenciesOverflowCleanup(t *testing.T) {
	g, err := newRV64FunctionDependencyGraph(10, 4)
	if err != nil {
		t.Fatal(err)
	}
	pc := prefinalizerTableMemoryCompile(t, false, false)
	var producers [9]*Instance
	var batch memoryImportAttachments
	var cleanup resourceCleanup
	callbacks := 0
	for i := range producers {
		producers[i], err = Instantiate(pc)
		if err != nil {
			t.Fatal(err)
		}
		defer producers[i].Close()
		memory, err := producers[i].ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		if err := rv64AcquireMemoryDependency(&batch, memory, g, &cleanup); err != nil {
			t.Fatal(err)
		}
		index := i
		producers[i].referenceLifetime().afterPhysicalRelease(func() {
			callbacks++
			if index == 0 {
				panic("first actual mapped memory producer")
			}
		})
		if err := producers[i].Close(); err != nil || producers[i].resourcesClosed {
			t.Fatal("pending acquisition lost actual producer", err)
		}
	}
	if batch.dependencies.rows.rows.n != 9 || batch.dependencies.rows.rows.index == nil {
		t.Fatal("fixture did not cover inline, overflow and indexed rows")
	}
	consumer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	consumer.beginConstruction(nil)
	railshotInstallMemoryDependencies(consumer, &batch)
	if err := railshotAdoptMemoryDependencies(consumer, true); err != nil {
		t.Fatal(err)
	}
	consumer.endConstruction()
	if err := consumer.Close(); !errors.Is(err, ErrCallbackPanic) || callbacks != len(producers) || !rv64ExistingMemoryDependencyRows(consumer).complete() || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || g.live != 0 {
		t.Fatal("first mapped producer panic skipped later owned units/native cleanup", err, callbacks)
	}
	for _, producer := range producers {
		if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil {
			t.Fatal("overflow producer native cleanup leak")
		}
	}
}

// A small private graph qualifies actual typed/anonymous/host acquisition and
// mapped lifetime exclusion without filling or resetting the live process graph.
// The consumer owns real native storage; these extra attachments deliberately
// do not claim an imported-memory execution profile (qualified above).
func TestRV64PublicMemoryDependenciesOperationLease(t *testing.T) {
	for _, selected := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("row%d", selected), func(t *testing.T) {
			g, err := newRV64FunctionDependencyGraph(3, 1)
			if err != nil {
				t.Fatal(err)
			}
			pc := prefinalizerTableMemoryCompile(t, false, false)
			var producers [2]*Instance
			var memories [3]*Memory
			for i := range producers {
				producers[i], err = Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producers[i].Close()
				memories[i], err = producers[i].ExportedMemory("memory")
				if err != nil {
					t.Fatal(err)
				}
			}
			memories[2], err = NewMemory(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer memories[2].Close()
			consumer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			var batch memoryImportAttachments
			var cleanup resourceCleanup
			for _, memory := range memories {
				if err := rv64AcquireMemoryDependency(&batch, memory, g, &cleanup); err != nil {
					t.Fatal(err)
				}
			}
			consumer.beginConstruction(nil)
			railshotInstallMemoryDependencies(consumer, &batch)
			if err := railshotAdoptMemoryDependencies(consumer, true); err != nil {
				t.Fatal(err)
			}
			consumer.endConstruction()
			rows := rv64ExistingMemoryDependencyRows(consumer)
			for _, producer := range producers {
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("actual imported root failed to retain producer", err)
				}
			}
			state := memories[selected].state.Load()
			state.mu.Lock()
			done := make(chan struct{})
			go func() { consumer.transferImportedMemoryAttachment(memories[selected]); close(done) }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				rows.mu.Lock()
				releasing := rows.rows.inline[selected].state == rv64MemoryDependencyReleasing
				rows.mu.Unlock()
				if releasing {
					break
				}
				if time.Now().After(deadline) {
					state.mu.Unlock()
					t.Fatal("operation did not claim its exact row")
				}
				time.Sleep(time.Millisecond)
			}
			if err := consumer.Close(); err != nil || consumer.resourcesClosed || consumer.finalizing || consumer.resourceRefs != 1 || consumer.jm == nil || consumer.ar == nil || consumer.eng == nil {
				state.mu.Unlock()
				t.Fatal("counter-in-flight operation lost native consumer lifetime", err)
			}
			state.mu.Unlock()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("operation root failed to complete terminal drain")
			}
			if !rows.complete() || !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || g.live != 0 {
				t.Fatal("operation publication/root drop failed to complete physical cleanup")
			}
			for _, memory := range memories {
				memory.state.Load().mu.Lock()
				count := memory.state.Load().importerCount()
				memory.state.Load().mu.Unlock()
				if count != 0 {
					t.Fatal("typed/anonymous/host counter leak", count)
				}
			}
			if _, ok := transferredImportAttachments.Load(consumer); ok {
				t.Fatal("concurrent transfer resurrected legacy state")
			}
			consumer.transferImportedMemoryAttachment(memories[selected])
		})
	}
}
