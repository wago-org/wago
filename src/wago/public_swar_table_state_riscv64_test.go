//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicSWARTableStateModule(borrowedMemory bool, memoryPosition, activeOffset int) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "v", wasm.V128, true), wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true)}
	if borrowedMemory {
		memory := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
		imports = append(append(append([][]byte(nil), imports[:memoryPosition]...), memory), imports[memoryPosition:]...)
	}
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128, wasm.V128, wasm.V128, wasm.I32, wasm.V128}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.V128, wasm.I32, wasm.V128}),
		wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
		wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128, wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType(nil, nil),
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{10, 0, 1, 2, 3, 4, 5, 6, 7, 8, 5}), wasmtest.Section(4, []byte{1, 0x70, 1, 2, 4})}
	if !borrowedMemory {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 4}))
	}
	literal := append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, literal))))
	var exports [][]byte
	for i, name := range []string{"call", "leaf", "private", "set", "store", "grow", "row", "init", "drop", "tablegrow"} {
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
	}
	exports = append(exports, wasmtest.ExportEntry("memory", 2, 0), wasmtest.ExportEntry("v", 3, 0), wasmtest.ExportEntry("flag", 3, 1))
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	active := append([]byte{0, 0x41}, wasmtest.SLEB32(int32(activeOffset))...)
	active = append(active, 0x0b, 1, 1)
	sections = append(sections, wasmtest.Section(9, wasmtest.Vec(active, []byte{1, 0, 1, 1})))
	bodies := [][]byte{
		{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x0b},
		{0x23, 0, 0x23, 2, 0x23, 1, 0x41, 3, 0xfd, 0, 0, 0, 0x0b},
		{0x23, 2, 0x20, 0, 0x24, 2, 0x0b},
		{0x23, 0, 0x23, 1, 0x20, 0, 0x24, 0, 0x20, 1, 0x24, 1, 0x0b},
		{0x41, 3, 0x20, 0, 0xfd, 11, 0, 0, 0x0b},
		{0x20, 0, 0x40, 0, 0x0b},
		{0x20, 0, 0xd2, 1, 0xd0, 0x70, 0x20, 1, 0x1c, 1, 0x70, 0x26, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 12, 1, 0, 0x0b},
		{0xfc, 13, 1, 0x0b},
		{0xd2, 1, 0x20, 0, 0xfc, 15, 0, 0x0b},
	}
	code := make([][]byte, len(bodies))
	for i, body := range bodies {
		code[i] = wasmtest.Code(body)
	}
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(code...)))...)
}

func TestRV64PublicSWAROwnedTableStateImportsContextAndRetirement(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		positions := []int{0}
		if borrowed {
			positions = []int{0, 1, 2}
		}
		for _, position := range positions {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("borrow%t-position%d-artifact%t", borrowed, position, artifact), func(t *testing.T) {
					memoryProducer, memory := importedMemoryProvider(t)
					producerCode, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
					if err != nil {
						t.Fatal(err)
					}
					defer producerCode.Close()
					producer, err := Instantiate(producerCode)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					vector, err := producer.ExportedGlobalObject("g4")
					if err != nil {
						t.Fatal(err)
					}
					flag, err := producer.ExportedGlobalObject("g0")
					if err != nil {
						t.Fatal(err)
					}
					imports := NewImports().Global("env", "v", vector).Global("env", "flag", flag)
					if borrowed {
						imports.Memory("env", "memory", memory)
					}
					c, err := Compile(NewRuntimeConfig().WithMemoryLimitPages(0), rv64PublicSWARTableStateModule(borrowed, position, 0))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					if artifact {
						blob, err := c.MarshalBinary()
						if err != nil {
							t.Fatal(err)
						}
						loaded, err := LoadTrustedArtifact(blob)
						if loaded != nil {
							defer loaded.Close()
						}
						if err != nil {
							t.Fatal(err)
						}
						c = loaded
					}
					instances := make([]*Instance, 2)
					for i := range instances {
						instances[i], err = Instantiate(c, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer instances[i].Close()
					}
					a, b := instances[0], instances[1]
					var limited *Instance
					if borrowed {
						limitedCode, err := Compile(NewRuntimeConfig().WithMemoryLimitPages(1), rv64PublicSWARTableStateModule(true, position, 0))
						if err != nil {
							t.Fatal(err)
						}
						defer limitedCode.Close()
						if artifact {
							blob, err := limitedCode.MarshalBinary()
							if err != nil {
								t.Fatal(err)
							}
							loaded, err := LoadTrustedArtifact(blob)
							if loaded != nil {
								defer loaded.Close()
							}
							if err != nil {
								t.Fatal(err)
							}
							limitedCode = loaded
						}
						// Resource quotas are execution policy and do not travel in artifacts.
						limited, err = Instantiate(limitedCode, InstantiateOptions{Imports: imports, memoryLimitPages: 1})
						if err != nil {
							t.Fatal(err)
						}
						defer limited.Close()
						importedGlobalInvoke(t, limited, "grow", []uint64{1}, 0)
					}
					private := [][]uint64{{0x7ff0000000000001, 0x80000000deadbeef}, {0xff8000017f800001, 0x7ff0000000000042}}
					payload := []uint64{0x8000000000000000, 0xffffffffffffffff}
					shared := []uint64{0x8899aabbccddeeff, 0x0807060504030201}
					for i, in := range instances {
						importedGlobalInvoke(t, in, "private", []uint64{0, 0}, private[i]...)
						importedGlobalInvoke(t, in, "store", nil, payload...)
					}
					flagValue := uint64(0xffffffff)
					call := func(in *Instance, p []uint64, index uint64) {
						t.Helper()
						want := []uint64{0x7ff0000000000001, 0xff8000017f800001, shared[0], shared[1], p[0], p[1], flagValue, payload[0], payload[1]}
						importedGlobalInvoke(t, in, "call", want, want[0], want[1], index|0xdeadbeef00000000)
					}
					call(a, private[0], 0)
					call(b, private[1], 0)
					importedGlobalInvoke(t, a, "set", append(slices.Clone(shared), flagValue), 0xff8000017f800001, 0x7ff0000000000042, 0xdeadbeef80000000)
					shared = []uint64{0xff8000017f800001, 0x7ff0000000000042}
					flagValue = 0x80000000
					call(b, private[1], 0)
					if binary.LittleEndian.Uint64(flag.cell) != flagValue {
						t.Fatal("table callee scalar global store not zero normalized")
					}
					// Borrowed memory produces identical row homes. The canonical context
					// must still prevent transplanting a sibling's valid native entry.
					if borrowed && binary.LittleEndian.Uint64(privateTableBytes(a)[24:32]) != binary.LittleEndian.Uint64(privateTableBytes(b)[24:32]) {
						t.Fatal("foreign-row fixture did not share row home")
					}
					if binary.LittleEndian.Uint64(privateTableBytes(a)[32:40]) == binary.LittleEndian.Uint64(privateTableBytes(b)[32:40]) {
						t.Fatal("foreign-row fixture did not have distinct descriptors")
					}
					before := slices.Clone(privateTableBytes(a))
					copy(privateTableBytes(a)[8:40], privateTableBytes(b)[8:40])
					if output, err := a.Invoke("call", 3, 4, 0); output != nil {
						t.Fatal("foreign table context produced results", output, err)
					} else {
						rv64RejectedTableEntry(t, err)
					}
					copy(privateTableBytes(a), before)
					call(a, private[0], 0)
					importedGlobalInvoke(t, a, "row", nil, 0, 0)
					if output, err := a.Invoke("call", 3, 4, 0); output != nil {
						t.Fatal("null table row produced results", output, err)
					} else {
						ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
					}
					call(b, private[1], 0)
					importedGlobalInvoke(t, a, "init", nil, 0, 0, 1)
					importedGlobalInvoke(t, a, "drop", nil)
					if _, err := a.Invoke("init", 1, 0, 1); err == nil {
						t.Fatal("dropped segment remained available")
					} else {
						ownedTableTrap(t, err, coreruntime.TrapTableOutOfBounds)
					}
					importedGlobalInvoke(t, b, "init", nil, 1, 0, 1)
					call(b, private[1], 1)
					importedGlobalInvoke(t, a, "tablegrow", []uint64{2}, 0xdeadbeef00000001)
					call(a, private[0], 2)
					importedGlobalInvoke(t, b, "tablegrow", []uint64{0xffffffff}, 0xdeadbeef00000003)
					importedGlobalInvoke(t, a, "grow", []uint64{1}, 0xdeadbeef00000001)
					wantPages := uint64(1)
					if borrowed {
						wantPages = 2
					}
					importedGlobalInvoke(t, b, "grow", []uint64{wantPages}, 0xdeadbeef00000000)
					if limited != nil {
						importedGlobalInvoke(t, limited, "tablegrow", []uint64{2}, 0)
						importedGlobalInvoke(t, limited, "grow", []uint64{2}, 0)
						importedGlobalInvoke(t, limited, "grow", []uint64{0xffffffff}, 1)
						importedGlobalInvoke(t, b, "grow", []uint64{2}, 0)
						if err := limited.Close(); err != nil {
							t.Fatal(err)
						}
					}
					call(a, private[0], 0)
					call(b, private[1], 0)
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := producerCode.Close(); err != nil {
						t.Fatal(err)
					}
					if borrowed {
						if err := memoryProducer.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if late, err := Instantiate(c, imports); late != nil || err == nil {
						t.Fatal("table attached retired state")
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					runtime.GC()
					call(a, private[0], 2)
					call(b, private[1], 1)
					if err := a.Close(); err != nil || !producer.hasPhysicalResources() || borrowed && memory.jobMemory() == nil {
						t.Fatal("first table borrower released shared state", err)
					}
					call(b, private[1], 1)
					if err := b.Close(); err != nil || producer.hasPhysicalResources() || borrowed && memory.jobMemory() != nil {
						t.Fatal("last table borrower retained state", err)
					}
				})
			}
		}
	}
}

func TestRV64PublicSWAROwnedTableStateImportsFailureRestoresProvider(t *testing.T) {
	producer, memory := importedMemoryProvider(t)
	vector := NewGlobalV128(rv64PublicVector(3, 4), true)
	defer vector.Close()
	flag := NewGlobalI32(7, true)
	defer flag.Close()
	imports := NewImports().Global("env", "v", vector).Global("env", "flag", flag).Memory("env", "memory", memory)
	c, err := Compile(nil, rv64PublicSWARTableStateModule(true, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		defer loaded.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	count := uint64(10)
	for _, code := range []*Compiled{c, loaded} {
		for range 2 {
			before := producer.jm.CaptureInstanceContext()
			if in, err := Instantiate(code, imports); in != nil || err == nil || errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("late active-element overflow did not fail initialization", err)
			}
			if producer.jm.CaptureInstanceContext() != before {
				t.Fatal("failed table initializer retained borrowed context pointers")
			}
			count++
			importedGlobalInvoke(t, producer, "bump", []uint64{count})
		}
	}
	if err := vector.Close(); err != nil {
		t.Fatal("failed table initializer leaked vector attachment", err)
	}
	if err := flag.Close(); err != nil {
		t.Fatal("failed table initializer leaked scalar attachment", err)
	}
	if err := producer.Close(); err != nil || memory.jobMemory() != nil {
		t.Fatal("failed table initializer leaked memory owner", err)
	}
}
