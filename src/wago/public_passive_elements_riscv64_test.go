//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PassiveElementsModule(t *testing.T, vector bool, payload string, drop int) []byte {
	t.Helper()
	source := rv64DeclarativeModule(t, vector, payload != "legacy", payload == "reference", drop)
	elements := make([][]byte, 256)
	for i := range elements {
		if payload == "legacy" {
			elements[i] = []byte{1, 0, 4, 0, 0, 0, 0}
		} else {
			value := []byte{0xd0, 0x70, 0x0b}
			if payload == "reference" {
				value = []byte{0xd2, 0, 0x0b}
			}
			elements[i] = append([]byte{5, 0x70}, wasmtest.Vec(value, value, value, value)...)
		}
	}
	return rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(elements...)))
}

func rv64PassiveDirectory(t *testing.T, in *Instance) []byte {
	t.Helper()
	if len(in.c.passiveElems) != 256 || in.jm.PassiveElemPtr() == 0 || in.tableDescPtr != 0 || in.c.tableCount() != 0 {
		t.Fatal("table-free passive directory shape")
	}
	return unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr())), 256*coreruntime.PassiveElemDescBytes)
}

func rv64AssertPassivePayload(t *testing.T, in *Instance, directory []byte, null bool) {
	t.Helper()
	var row []byte
	var zeroPayload [4 * coreruntime.TableEntryBytes]byte
	if !null {
		if len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
			t.Fatal("missing passive function descriptor")
		}
		row = in.funcRefDescs[coreruntime.FuncRefDescBytes : coreruntime.FuncRefDescBytes+coreruntime.TableEntryBytes]
	}
	for i := 0; i < 256; i++ {
		desc := directory[i*16 : (i+1)*16]
		ptr := binary.LittleEndian.Uint64(desc)
		if ptr == 0 || binary.LittleEndian.Uint32(desc[8:]) != 4 || binary.LittleEndian.Uint32(desc[12:]) != 0 || in.c.passiveElems[i].Mode != ElemModePassive || len(in.c.passiveElems[i].Values) != 4 {
			t.Fatal("passive original payload/count", i)
		}
		payload := unsafe.Slice((*byte)(offHeapPtr(uintptr(ptr))), 4*coreruntime.TableEntryBytes)
		if null {
			if !bytes.Equal(payload, zeroPayload[:]) {
				t.Fatal("null passive payload", i)
			}
		} else {
			for n := 0; n < 4; n++ {
				if !bytes.Equal(payload[n*32:(n+1)*32], row) {
					t.Fatal("canonical passive reference payload", i, n)
				}
			}
		}
	}
}

func TestRV64PublicPassiveElementsWithoutTable(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, payload := range []string{"legacy", "null", "reference"} {
			for _, drop := range []int{-1, 0, 255} {
				for _, artifact := range []bool{false, true} {
					name := fmt.Sprintf("passive-v%t-%s-drop%d", vector, payload, drop)
					t.Run(fmt.Sprintf("%s-artifact%t", name, artifact), func(t *testing.T) {
						c := rv64TableStageCompile(t, name, rv64PassiveElementsModule(t, vector, payload, drop), artifact)
						rt := NewRuntime()
						defer rt.Close()
						in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
						defer in.Close()
						other := rv64NativeTokenInstantiate(t, rt, c, NewImports())
						defer other.Close()
						if (!in.c.needsFuncRefDescs()) != (payload == "null") || payload == "null" && !rv64HeaderOnlyContext(in) || len(in.globalCells) != 0 {
							t.Fatal("passive descriptor/global allocation")
						}
						directory := rv64PassiveDirectory(t, in)
						otherDirectory := rv64PassiveDirectory(t, other)
						if in.jm.PassiveElemPtr() == other.jm.PassiveElemPtr() {
							t.Fatal("shared instance drop state")
						}
						rv64AssertPassivePayload(t, in, directory, payload == "null")
						expected, untouched := bytes.Clone(directory), bytes.Clone(otherDirectory)
						if drop >= 0 {
							binary.LittleEndian.PutUint32(expected[drop*16+8:], 0)
						}
						c.Close()
						want := uint64(7)
						if payload == "reference" {
							want = 0
						}
						for repeat := 0; repeat < 3; repeat++ {
							importedGlobalInvoke(t, in, "carrier", []uint64{want}, 0xdeadbeef00000000)
							importedGlobalInvoke(t, in, "drop", []uint64{7})
						}
						if !bytes.Equal(directory, expected) || !bytes.Equal(otherDirectory, untouched) || len(rt.refStore.byToken) != 0 || len(rt.refStore.byIdentity) != 0 {
							t.Fatal("drop changed payload pointers, padding, unrelated lengths, another instance, or public tokens")
						}
						in.Close()
						rv64AssertPassivePayload(t, other, otherDirectory, payload == "null")
					})
				}
			}
		}
	}
}

func TestRV64PublicPassiveElementImportedTargets(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, kind := range []string{"callback", "host", "native"} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-%s-artifact%t", vector, kind, artifact), func(t *testing.T) {
					types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
					if vector {
						types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
					}
					source := rv64PassiveElementsModule(t, false, "reference", 255)
					imports := wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)))
					source = rv64ReplaceTableStageSection(t, source, 1, append(wasmtest.Section(1, wasmtest.Vec(types...)), imports...))
					source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{2, 1, 2}))
					source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x42, 7, 0x10, 0, 0x1a, 0xd1, 0x0b}), wasmtest.Code([]byte{0xfc, 13, 0xff, 1, 0xfc, 13, 0xff, 1, 0x41, 7, 0x0b}))))
					c := rv64TableStageCompile(t, fmt.Sprintf("passive-import-v%t", vector), source, artifact)
					rt := NewRuntime()
					defer rt.Close()
					calls := 0
					var binding any = slotHostFunc(func(_ HostModule, p, r []uint64) { calls++; copy(r, p) })
					var host *HostFuncRef
					var producer *Instance
					if kind == "host" {
						host = rv64HostTokenOwner(t, rt, ValI64, binding.(slotHostFunc))
						defer host.Close()
						binding = host
					} else if kind == "native" {
						pc := rv64TableStageCompile(t, "decl-native-bare", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I64}), artifact)
						producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						defer producer.Close()
						var err error
						binding, err = producer.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						pc.Close()
					}
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
					defer in.Close()
					if host != nil && host.Close() == nil {
						t.Fatal("host lost attached owner")
					}
					if producer != nil {
						producer.Close()
					}
					c.Close()
					directory := rv64PassiveDirectory(t, in)
					rv64AssertPassivePayload(t, in, directory, false)
					expected := bytes.Clone(directory)
					binary.LittleEndian.PutUint32(expected[255*16+8:], 0)
					for repeat := 0; repeat < 3; repeat++ {
						importedGlobalInvoke(t, in, "carrier", []uint64{0}, 0)
						importedGlobalInvoke(t, in, "drop", []uint64{7})
					}
					if !bytes.Equal(directory, expected) || kind != "native" && calls != 3 || len(rt.refStore.byToken) != 0 {
						t.Fatal("imported passive lifetime/drop/token contract", calls)
					}
					in.Close()
					if host != nil && host.Close() != nil {
						t.Fatal("host attachment retained after close")
					}
				})
			}
		}
	}
}

func TestRV64PublicPassiveElementFeatureGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source feature admission")
	}
	for _, vector := range []bool{false, true} {
		legacy := rv64PassiveElementsModule(t, vector, "legacy", -1)
		c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), legacy)
		if err != nil {
			t.Fatal("unused legacy passive payload invented reference feature", err)
		}
		if !c.needsFuncRefDescs() || len(c.passiveElems) != 256 {
			t.Fatal("unused legacy passive state omitted")
		}
		c.Close()
		for _, source := range [][]byte{legacy, rv64PassiveElementsModule(t, vector, "null", -1)} {
			c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureBulkMemoryOperations, false), source)
			if c != nil {
				c.Close()
			}
			if c != nil || err == nil {
				t.Fatal("passive segment accepted without bulk feature")
			}
		}
		for _, source := range [][]byte{rv64PassiveElementsModule(t, vector, "null", -1), rv64PassiveElementsModule(t, vector, "legacy", 255)} {
			c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), source)
			if c != nil {
				c.Close()
			}
			if c != nil || err == nil {
				t.Fatal("reference operation accepted without reference feature")
			}
		}
	}
}

func TestRV64PublicPassiveGlobalInitializersWithoutTable(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-imported%t-artifact%t", vector, imported, artifact), func(t *testing.T) {
					source := rv64ElementGlobalModule(vector, imported, false, false, 5, 0)
					source = rv64ReplaceTableStageSection(t, source, 4, nil)
					source = rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0), wasmtest.ExportEntry("drop", 0, 2))))
					source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(
						wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}),
						wasmtest.Code([]byte{0xfc, 13, 0, 0xfc, 13, 0, 0x41, 7, 0x0b}),
					)))
					rt := NewRuntime()
					defer rt.Close()
					imports := NewImports()
					var producer *Instance
					var pc *Compiled
					if imported {
						pc = rv64TableStageCompile(t, fmt.Sprintf("ref-no-table-leaf-vector%t", vector), rv64ReferenceNoTableModule(vector, false, false), artifact)
						producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						defer producer.Close()
						g, err := producer.ExportedGlobalObject("reference")
						if err != nil {
							t.Fatal(err)
						}
						imports.Global("env", "ref", g)
					}
					c := rv64TableStageCompile(t, fmt.Sprintf("passive-global-v%t-imported%t", vector, imported), source, artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, imports)
					defer in.Close()
					c.Close()
					if producer != nil {
						producer.Close()
						pc.Close()
					}
					if in.tableDescPtr != 0 || in.c.tableCount() != 0 || len(in.c.passiveElems) != 1 || in.jm.PassiveElemPtr() == 0 {
						t.Fatal("passive globals fabricated table state")
					}
					owner := in
					if imported {
						owner = producer
						if !rv64HeaderOnlyContext(in) || !producer.hasPhysicalResources() {
							t.Fatal("foreign raw descriptor ownership")
						}
					}
					bits := readGlobalObject(in.globalCells[0], ValFuncRef)
					canonical, ok := rv64PublishedDescriptor(owner, bits)
					if !ok {
						t.Fatal("global descriptor has no retained owner")
					}
					directory := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr())), coreruntime.PassiveElemDescBytes)
					ptr := binary.LittleEndian.Uint64(directory)
					if ptr == 0 || binary.LittleEndian.Uint32(directory[8:]) != 3 {
						t.Fatal("passive global payload extent")
					}
					payload := unsafe.Slice((*byte)(offHeapPtr(uintptr(ptr))), 3*coreruntime.TableEntryBytes)
					var zero [coreruntime.TableEntryBytes]byte
					if !bytes.Equal(payload[:32], canonical[:32]) || !bytes.Equal(payload[32:64], canonical[:32]) || !bytes.Equal(payload[64:], zero[:]) {
						t.Fatal("copied global passive rows changed canonical identity")
					}
					before, expected := bytes.Clone(payload), bytes.Clone(directory)
					binary.LittleEndian.PutUint32(expected[8:], 0)
					for repeat := 0; repeat < 3; repeat++ {
						importedGlobalInvoke(t, in, "drop", []uint64{7})
					}
					if !bytes.Equal(payload, before) || !bytes.Equal(directory, expected) || len(rt.refStore.byToken) != 0 || len(rt.refStore.byIdentity) != 0 {
						t.Fatal("global passive drop changed payload, descriptor pointer, or public tokens")
					}
					if imported && !producer.hasPhysicalResources() {
						t.Fatal("drop released live global attachment")
					}
					in.Close()
					if imported && producer.hasPhysicalResources() {
						t.Fatal("consumer close retained raw producer descriptor")
					}
				})
			}
		}
	}
}
