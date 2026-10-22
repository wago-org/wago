//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// prepare and load run in separate processes. The load process can exclude the
// compiler, so no source receipt or compiler registry can qualify these images.
func TestRV64PortableFeatureArtifacts(t *testing.T) {
	mode, dir := os.Getenv("WAGO_RV64_PORTABLE_FEATURE_MODE"), os.Getenv("WAGO_RV64_PORTABLE_FEATURE_DIR")
	for _, name := range []string{"reference-call", "reference-tail", "indexed-table-tail", "unreachable-reference-tail", "rooted-tail", "segment-arrays", "exceptions", "owned-exceptions", "owned-functions", "function-struct", "function-array", "owned-null", "threads", "plugin", "plugin-v128", "table64-capacity", "table64-mixed", "gc-memory32", "gc-memory64", "gc-memory32-unbounded", "gc-memory64-unbounded", "gc-memory32-large", "gc-memory-shared", "gc-memory-shared-call"} {
		t.Run(name, func(t *testing.T) {
			var blob []byte
			var err error
			path := filepath.Join(dir, name+".wago")
			if mode == "load" {
				blob, err = os.ReadFile(path)
			} else {
				blob = rv64PreparePortableFeature(t, name)
				if mode == "prepare" {
					err = os.WriteFile(path, blob, 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			c, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !rv64ArtifactContractValid(c.executionView()) {
				t.Fatal("loaded feature has no portable execution contract")
			}
			if name == "table64-capacity" {
				view := c.executionView()
				facts := view.rv64ExecutionTableCapacities()
				if len(facts) != 1 || facts[0] != 0 || rv64ExecutionContractSize(view) != rv64ExecutionContractBytes+8 {
					t.Fatalf("reserved capacity or v2 trailer lost: %v", facts)
				}
				candidate := cloneCompiledMetadata(view)
				indexes := *candidate.loadCompileIndexes()
				candidate.validateMemo = &validateMemo{compileIndexes: &indexes}
				candidate.setRV64ExecutionTableCapacities([]uint32{facts[0] + 1})
				if rv64ArtifactContractValid(candidate) {
					t.Fatal("changed reserved capacity kept execution binding")
				}
			}
			gcMemory := strings.HasPrefix(name, "gc-memory")
			options := InstantiateOptions{}
			var memory *Memory
			portableCalls := 0
			if name == "threads" || name == "gc-memory-shared" || name == "gc-memory-shared-call" {
				maximum := uint32(1)
				if gcMemory {
					maximum = 2
				}
				memory, err = NewSharedMemory(1, maximum)
				if err != nil {
					t.Fatal(err)
				}
				defer memory.Close()
				options.Imports = testImports("env.memory", memory)
			}
			if name == "plugin" {
				options.Imports = testImports("wago:instr/machine.i32.identity", func(v int32) int32 { portableCalls++; return -99 })
			}
			if name == "plugin-v128" {
				fallback := slotHostFunc(func(HostModule, []uint64, []uint64) { panic("native custom artifact used its host fallback") })
				options.Imports = NewImports().Function("rv64-custom", "new", instructionNativeCustomBinding(fallback, nil, []ValType{ValV128})).Function("rv64-custom", "low", instructionNativeCustomBinding(fallback, []ValType{ValV128}, []ValType{ValI32}))
			}
			if gcMemory || name == "rooted-tail" || name == "segment-arrays" || name == "owned-exceptions" || name == "owned-functions" || name == "function-struct" || name == "function-array" {
				options.GC = GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
			}
			in, err := Instantiate(c, options)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			invoke := func(export string, want []uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(export, args...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatalf("%s(%v)=%v,%v; want %v", export, args, got, err, want)
				}
			}
			if gcMemory && name != "gc-memory-shared" && name != "gc-memory-shared-call" {
				invoke("grow", []uint64{1}, 1)
				invoke("size", []uint64{2})
			}
			for range 16 {
				switch name {
				case "gc-memory32", "gc-memory64", "gc-memory32-unbounded", "gc-memory64-unbounded", "gc-memory32-large", "gc-memory-shared", "gc-memory-shared-call":
					invoke("run", []uint64{49})
					invoke("probe", []uint64{7}, 0)
					if name == "gc-memory64" || name == "gc-memory64-unbounded" {
						if _, err := in.Invoke("probe", uint64(1)<<32); err == nil {
							t.Fatal("memory64 address wrapped to memory32")
						}
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				case "reference-call", "reference-tail":
					invoke("call", []uint64{0x80000001}, 0xfeedface80000001, 0)
					if _, err := in.Invoke("call", uint64(7), uint64(1)); err == nil {
						t.Fatal("null reference did not trap")
					}
				case "indexed-table-tail":
					invoke("call", []uint64{42}, 0)
					if _, err := in.Invoke("call", uint64(1)); err == nil {
						t.Fatal("out-of-bounds indirect tail did not trap")
					}
				case "unreachable-reference-tail":
					if _, err := in.Invoke("trap"); err == nil {
						t.Fatal("unreachable reference tail did not trap")
					}
					invoke("good", []uint64{42})
				case "rooted-tail":
					invoke("run", []uint64{42})
				case "segment-arrays":
					invoke("run", []uint64{0xfffffffe, 1, 4, 0xfffffffd})
				case "exceptions":
					invoke("catch", []uint64{37}, 3, 7, 0)
					if _, err := in.Invoke("uncaught", uint64(3), uint64(7), uint64(0)); err == nil {
						t.Fatal("uncaught exception did not trap")
					}
				case "owned-exceptions":
					got, err := in.Invoke("make", uint64(123))
					if err != nil || len(got) != 1 || got[0] == 0 {
						t.Fatalf("make(123)=%v,%v", got, err)
					}
					token := got[0]
					invoke("get", []uint64{123}, token)
					if err := in.ReleaseGCRef(GCRef{token: token}); err != nil {
						t.Fatal(err)
					}
				case "function-struct", "function-array":
					invoke("run", []uint64{42})
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				case "owned-functions":
					invoke("save", nil)
					invoke("held", []uint64{42})
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					invoke("run", []uint64{42})
				case "owned-null":
					for i := range rv64ExceptionNullHeaps {
						invoke(string([]byte{'a' + byte(i)}), []uint64{0})
					}
				case "threads":
					invoke("wait32", []uint64{uint64(memoryWaitNotEqual)}, 0, 1, ^uint64(0))
					invoke("wait64", []uint64{uint64(memoryWaitTimedOut)}, 0, 0, 0)
					invoke("notify", []uint64{0}, 0, 1)
				case "plugin":
					invoke("run", []uint64{0x80000001}, 0xfeedface80000001)
					if portableCalls != 0 {
						t.Fatal("native plugin called its portable handler")
					}
				case "plugin-v128":
					invoke("run", []uint64{0x55667788})
				case "table64-mixed":
					invoke("copy", nil, 0, 0, 2)
					invoke("float", []uint64{0x3ff0000000000000}, 0x3ff0000000000000, 0)
					invoke("vector", []uint64{0x1122334455667788, 0x9988776655443322}, 0x1122334455667788, 0x9988776655443322, 1)
					invoke("copy", nil, 2, 0, 2)
					invoke("init", nil, 2, 0, 2)
					invoke("fill", nil, 2, 2)
					invoke("is_null", []uint64{1}, 2)
					invoke("set_null", nil, 3)
					invoke("copy64", nil, 2, 0, 2)
					invoke("grow", []uint64{4}, 0)
					invoke("size", []uint64{4})
					if _, err := in.Invoke("is_null", ^uint64(0)); err == nil {
						t.Fatal("upper-bit table64 index did not trap")
					}
				case "table64-capacity":
					invoke("size", []uint64{0})
				}
			}
			if name == "table64-mixed" {
				invoke("drop", nil)
				invoke("init", nil, 4, 0, 0)
			}
			if name == "indexed-table-tail" {
				fn, err := in.WasmFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				if allocations := testing.AllocsPerRun(100, func() {
					got, err := fn.Invoke(uint64(0))
					if err != nil || len(got) != 1 || got[0] != 42 {
						panic("warm indexed tail result")
					}
				}); allocations != 0 {
					t.Fatalf("warm indexed tail allocations=%g", allocations)
				}
				t.Log("warm indexed tail allocations=0")
			}
			again, err := c.MarshalBinary()
			if err != nil || !slices.Equal(again, blob) {
				t.Fatalf("feature metadata changed after load: %v", err)
			}
			t.Logf("%s loaded and executed; artifact=%d bytes", name, len(blob))
		})
	}
}
