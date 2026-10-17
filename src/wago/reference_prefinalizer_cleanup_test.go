//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func prefinalizerTableMemorySource(consumer bool) []byte {
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))}
	if consumer {
		memory := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 0, 1)
		table := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 0, 1)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(memory, table)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{1, 0}))
	if !consumer {
		sections = append(sections, ownedTableDeclaration(1))
	}
	// The consumer has an additional owned mapping whose cleanup must survive
	// the imported owner's physical callback panic during table-driven transfer.
	sections = append(sections, wasmtest.Section(5, []byte{1, 0, 1}))
	exports := [][]byte{wasmtest.ExportEntry("safe", 0, 0)}
	if !consumer {
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0), wasmtest.ExportEntry("table", 1, 0))
	} else {
		exports = append(exports, wasmtest.ExportEntry("secondary", 2, 1))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if consumer {
		sections = append(sections, ownedTableElements(0, 0))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}))))
	return wasmtest.Module(sections...)
}

func prefinalizerTableMemoryCompile(t *testing.T, consumer, artifact bool) *Compiled {
	t.Helper()
	mode, dir := os.Getenv("WAGO_PREFINALIZER_ARTIFACT_MODE"), os.Getenv("WAGO_PREFINALIZER_ARTIFACT_DIR")
	name := fmt.Sprintf("prefinalizer-memory-consumer%t.wago", consumer)
	if mode == "load" {
		if !artifact {
			t.Fatal("compiler-free runner selected source path")
		}
		blob, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	c, err := Compile(artifactTestConfig(artifact || mode == "prepare"), prefinalizerTableMemorySource(consumer))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if artifact || mode == "prepare" {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { loaded.Close() })
			return loaded
		}
	}
	return c
}

func TestPhysicalPrefinalizerTableMemoryTransferPanicCompletes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, retained := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-retained%t", artifact, retained), func(t *testing.T) {
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
				consumer, err := Instantiate(prefinalizerTableMemoryCompile(t, true, artifact), NewImports().Memory("env", "memory", memory).Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if got, err := consumer.Invoke("safe"); err != nil || len(got) != 1 || got[0] != 17 {
					t.Fatal("genuine mapped table/memory fixture", got, err)
				}
				if consumer.memoryDir == nil || len(consumer.memoryDir.memories) != 2 || !consumer.memoryDir.owns[1] {
					t.Fatal("consumer must own a later secondary memory obligation")
				}
				secondary, err := consumer.ExportedMemory("secondary")
				if err != nil || secondary != consumer.memoryDir.memories[1] {
					t.Fatal("genuine secondary memory handle", err)
				}
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("producer must remain physically retained by both imports", err)
				}
				var visited []int
				componentClaim := compiledArchitecture == 4
				var firstObserved any
				first, later := new(int), new(int)
				producer.referenceLifetime().afterPhysicalRelease(func() {
					defer func() { firstObserved = recover(); panic(firstObserved) }()
					visited = append(visited, 0)
					if retained {
						// A closed RV64 dependency component claims every member
						// before its physical journal. Such a claim is terminal;
						// callbacks cannot resurrect an already claimed member.
						if componentClaim && !consumer.resourcesClosed {
							panic("RV64 component was not physically claimed")
						}
						acquired := consumer.retainResourceRootForFinalization()
						if componentClaim == acquired {
							panic("consumer operation lease disagrees with physical claim")
						}
					}
					panic(first)
				})
				consumer.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 1); panic(later) })
				consumer.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 2) })
				err = consumer.Close()
				if firstObserved != first {
					t.Fatal("first physical callback panic changed", firstObserved)
				}
				if !errors.Is(err, ErrCallbackPanic) {
					t.Fatal("pre-finalization panic lost its Close outcome", err)
				}
				if retained && !componentClaim {
					if consumer.finalizing || consumer.resourcesClosed || consumer.resourceRefs != 1 || consumer.jm == nil || secondary.jobMemory() == nil || !reflect.DeepEqual(visited, []int{0}) {
						t.Fatal("deferred physical cleanup kept its finalizing claim or lost the operation lease", visited)
					}
					value, panicked := cleanupTestPanic(consumer.releaseResourceRoot)
					if !panicked || value != later {
						t.Fatal("later root release lost its exact physical panic", value, panicked)
					}
				}
				if !reflect.DeepEqual(visited, []int{0, 1, 2}) {
					t.Fatal("pre-finalization panic lost later physical callbacks", err, visited)
				}
				for _, in := range []*Instance{producer, consumer} {
					if in.finalizing || !in.resourcesClosed || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil || in.base != 0 || in.nativeContext != 0 {
						t.Fatal("pre-finalization panic stranded claim or native storage")
					}
				}
				state := memory.state.Load()
				state.mu.Lock()
				count := state.importerCount()
				state.mu.Unlock()
				if count != 0 || memory.jm != nil {
					t.Fatal("transferred memory counter did not drain", count)
				}
				secondaryState := secondary.state.Load()
				secondaryState.mu.Lock()
				secondaryClosed := secondaryState.has(memoryStateClosed)
				secondaryState.mu.Unlock()
				if !secondaryClosed || secondary.jobMemory() != nil {
					t.Fatal("later owned secondary memory was not released")
				}
			})
		}
	}
}
