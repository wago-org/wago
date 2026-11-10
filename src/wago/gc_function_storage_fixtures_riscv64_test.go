//go:build linux && riscv64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The runner discards its container before the final allocation. A live raw
// function must retain its physical owner without retaining that container.
func rv64FunctionStorageFixture(array, local, vector bool, mutation string) []byte {
	return rv64FunctionStorageInputFixture(array, local, vector, mutation, false)
}

func rv64FunctionStorageInputFixture(array, local, vector bool, mutation string, foreign bool) []byte {
	storage := []byte{0x5f, 1, 0x63, 0, 1}
	if array {
		storage = []byte{0x5e, 0x63, 0, 1}
	}
	types := wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, storage, []byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 1, 0x7f}))
	body := []byte{}
	locals := []byte{0}
	functionLocal, objectLocal, functionType := byte(0), byte(0), byte(0)
	value := []byte{0xd2, 0}
	if foreign {
		functionLocal, objectLocal, functionType = 1, 1, 3
		value = []byte{0x20, 0}
	}
	if mutation == "" {
		body = append(body, value...)
		if array {
			body = append(body, 0xfb, 8, 1, 1)
		} else {
			body = append(body, 0xfb, 0, 1)
		}
	} else {
		locals = []byte{1, 1, 0x63, 1}
		functionLocal = objectLocal + 1
		if array {
			body = append(body, 0x41, 1, 0xfb, 7, 1)
		} else {
			body = append(body, 0xfb, 1, 1)
		}
		body = append(body, 0x21, objectLocal, 0x20, objectLocal)
		if array {
			body = append(body, 0x41, 0)
		}
		switch mutation {
		case "set":
			body = append(body, value...)
			if array {
				body = append(body, 0xfb, 14, 1)
			} else {
				body = append(body, 0xfb, 5, 1, 0)
			}
		case "fill":
			body = append(body, value...)
			body = append(body, 0x41, 1, 0xfb, 16, 1)
		case "init":
			body = append(body, 0x41, 0, 0x41, 1, 0xfb, 19, 1, 0)
		}
		body = append(body, 0x20, objectLocal)
	}
	if array {
		body = append(body, 0x41, 0, 0xfb, 11, 1)
	} else {
		body = append(body, 0xfb, 2, 1, 0)
	}
	if mutation != "" {
		body = append(body, 0xd0, 1, 0x21, objectLocal)
	}
	if foreign {
		body = append(body, 0xd0, 0, 0x21, 0)
	}
	if local {
		if mutation == "" {
			locals = []byte{1, 1, 0x63, 0}
		} else {
			locals = []byte{2, 1, 0x63, 1, 1, 0x63, 0}
		}
		body = append(body, 0x21, functionLocal)
	}
	if vector {
		body = append(body, 0xfd, 12)
		body = append(body, make([]byte, 16)...)
		body = append(body, 0x1a)
	}
	body = append(body, 0xfb, 1, 2, 0x1a)
	if local {
		body = append(body, 0x20, functionLocal)
	}
	body = append(body, 0x14, 0, 0x0b)
	code := append(locals, body...)
	// The passive typed segment also declares ref.func 0 for all variants.
	element := []byte{5, 0x63, 0, 1, 0xd2, 0, 0x0b}
	return wasmtest.Module(types,
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{functionType})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(element)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestRV64FunctionStorageFixtureTyping(t *testing.T) {
	testRV64FunctionStorageFixtureTyping(t, false)
}

func TestRV64ForeignFunctionStorageFixtureTyping(t *testing.T) {
	testRV64FunctionStorageFixtureTyping(t, true)
}

func testRV64FunctionStorageFixtureTyping(t *testing.T, foreign bool) {
	for _, array := range []bool{false, true} {
		mutations := []string{"", "set"}
		if array {
			mutations = append(mutations, "fill", "init")
		}
		for _, mutation := range mutations {
			for _, local := range []bool{false, true} {
				for _, vector := range []bool{false, true} {
					t.Run(fmt.Sprintf("array=%v/%s/local=%v/vector=%v", array, mutation, local, vector), func(t *testing.T) {
						m, err := wasm.DecodeModule(rv64FunctionStorageInputFixture(array, local, vector, mutation, foreign))
						if err != nil {
							t.Fatal(err)
						}
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true, CompactImports: true}, 1, wasm.ValidationLimits{}); err != nil {
							t.Fatal(err)
						}
						plan, err := wasm.BuildCollectorOperandRoots(m, 1)
						if err != nil {
							t.Fatal(err)
						}
						found := false
						r := wasm.ReaderFrom(m.Code[1].BodyBytes)
						for r.HasNext() {
							pc := m.Code[1].LocalDeclBytes + uint32(r.Offset())
							op, err := r.Byte()
							if err != nil {
								t.Fatal(err)
							}
							var imm wasm.InstructionImmediate
							if err := wasm.ClassifyInstructionImmediateInto(&r, op, &imm); err != nil {
								t.Fatal(err)
							}
							if imm.Kind == wasm.InstrStructNewDefault && imm.Index == 2 {
								functions, ok := plan.FunctionsAt(pc)
								collector, collectorOK := plan.At(pc)
								want := 1
								if local {
									want = 0
								}
								if !ok || !collectorOK || len(functions) != want || len(collector) != 0 || want == 1 && functions[0] != 0 {
									t.Fatalf("extracted function roots=%v collector=%v", functions, collector)
								}
								found = true
							}
							if wasm.FunctionStorageMutation(m, imm.Kind, imm.Index, imm.Index2) {
								functions, ok := plan.FunctionsAt(pc)
								collector, collectorOK := plan.At(pc)
								want := 1
								if mutation == "init" {
									want = 0
								}
								if !ok || !collectorOK || len(functions) != want || len(collector) != 1 || collector[0] != 0 {
									t.Fatalf("mutation function roots=%v collector=%v", functions, collector)
								}
							}
						}
						if !found {
							t.Fatal("missing allocation after container discard")
						}
					})
				}
			}
		}
	}
}

func TestRV64FunctionEnvironmentRootGrammar(t *testing.T) {
	c := &Compiled{requiredFeatures: CoreFeatureGC, codeCache: &compiledCodeCache{gcStructProduct: stagedGCStructGeneric}, validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}}, GCTypeDescs: []gc.TypeDesc{{Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageFuncRefNull}}}}}
	c.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
	if !c.functionEnvironmentFrameRoots() || !c.usesManagedFunctionStorage() {
		t.Fatal("qualified generic function-storage grammar rejected")
	}
	for name, change := range map[string]func(*Compiled){
		"ABI":     func(c *Compiled) { c.codeCache.setNativeGCABIVersion(0) },
		"maps":    func(c *Compiled) { c.validateMemo.gcFrameRoots = nil },
		"product": func(c *Compiled) { c.codeCache.gcStructProduct = 0 },
		"feature": func(c *Compiled) { c.requiredFeatures = 0 },
		"storage": func(c *Compiled) { c.GCTypeDescs = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *c
			cache, memo := *c.codeCache, *c.validateMemo
			candidate.codeCache, candidate.validateMemo = &cache, &memo
			change(&candidate)
			if name == "maps" && !candidate.usesManagedFunctionStorage() {
				t.Fatal("runtime storage classifier required frame maps")
			}
			if name != "maps" && candidate.usesManagedFunctionStorage() {
				t.Fatal("declaration-only or incomplete storage mode accepted")
			}
			if candidate.functionEnvironmentFrameRoots() {
				t.Fatal("incomplete function-storage grammar accepted")
			}
		})
	}
	// This grammar check grants no executable authority. The final source or
	// artifact contract still binds the complete native image and metadata.
}
