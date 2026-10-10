//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type rv64CompactImportForm uint8

const (
	rv64CompactMixed rv64CompactImportForm = iota
	rv64CompactSharedMemory
)

func rv64CompactFunctionImport(name string) []byte {
	return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), byte(wasm.ExternFunc), 0)
}

func rv64CompactMemoryImport(name string, min, max byte) []byte {
	return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), byte(wasm.ExternMem), 1, min, max)
}

func rv64CompactGlobalImport(name string) []byte {
	return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), byte(wasm.ExternGlobal), 0x7f, 0)
}

func rv64CompactImportModule(form rv64CompactImportForm, compact, swar bool) []byte {
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	functions := [][]byte{wasmtest.ULEB(1)}
	exports := [][]byte{wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1)}
	bodies := [][]byte{wasmtest.Code([]byte{
		0x20, 0, 0x10, 0, // host(arg)
		0x23, 0, 0x6a, // + bias
		0x3f, 0, 0x6a, // + memory.size a
		0x3f, 1, 0x6a, // + memory.size b
		0x0b,
	})}
	if swar {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions = append(functions, wasmtest.ULEB(2))
		exports = append(exports, wasmtest.ExportEntry("vector", byte(wasm.ExternFunc), 2))
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x0b}))
	}

	var importPayload []byte
	if !compact {
		entries := [][]byte{
			rv64CompactFunctionImport("host"),
			rv64CompactMemoryImport("a", 1, 2),
		}
		if form == rv64CompactMixed {
			entries = append(entries, rv64CompactGlobalImport("bias"), rv64CompactMemoryImport("b", 2, 3))
		} else {
			entries = append(entries, rv64CompactMemoryImport("b", 2, 3), rv64CompactGlobalImport("bias"))
		}
		importPayload = wasmtest.Vec(entries...)
	} else if form == rv64CompactMixed {
		importPayload = append(wasmtest.ULEB(4), wasmtest.Name("env")...)
		importPayload = append(importPayload, 0, 0x7f, 4)
		importPayload = append(importPayload, wasmtest.Name("host")...)
		importPayload = append(importPayload, byte(wasm.ExternFunc), 0)
		importPayload = append(importPayload, wasmtest.Name("a")...)
		importPayload = append(importPayload, byte(wasm.ExternMem), 1, 1, 2)
		importPayload = append(importPayload, wasmtest.Name("bias")...)
		importPayload = append(importPayload, byte(wasm.ExternGlobal), 0x7f, 0)
		importPayload = append(importPayload, wasmtest.Name("b")...)
		importPayload = append(importPayload, byte(wasm.ExternMem), 1, 2, 3)
	} else {
		importPayload = append(wasmtest.ULEB(4), rv64CompactFunctionImport("host")...)
		importPayload = append(importPayload, wasmtest.Name("env")...)
		importPayload = append(importPayload, 0, 0x7e, byte(wasm.ExternMem), 2)
		importPayload = append(importPayload, wasmtest.Name("a")...)
		importPayload = append(importPayload, 1, 1, 2)
		importPayload = append(importPayload, wasmtest.Name("b")...)
		importPayload = append(importPayload, 1, 2, 3)
		importPayload = append(importPayload, rv64CompactGlobalImport("bias")...)
	}

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, importPayload),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestRV64PublicCompactImportsScalarAndSWAR(t *testing.T) {
	forms := []struct {
		name string
		form rv64CompactImportForm
	}{
		{name: "mixed", form: rv64CompactMixed},
		{name: "shared-memory", form: rv64CompactSharedMemory},
	}
	for _, tc := range forms {
		for _, swar := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-swar%t-artifact%t", tc.name, swar, artifact), func(t *testing.T) {
					ordinary := rv64TableStageCompile(t, fmt.Sprintf("compact-import-%s-swar%t-ordinary", tc.name, swar), rv64CompactImportModule(tc.form, false, swar), artifact)
					compact := rv64TableStageCompile(t, fmt.Sprintf("compact-import-%s-swar%t-compact", tc.name, swar), rv64CompactImportModule(tc.form, true, swar), artifact)
					if !bytes.Equal(ordinary.code, compact.code) || !slices.Equal(ordinary.Entry, compact.Entry) || !slices.Equal(ordinary.InternalEntry, compact.InternalEntry) {
						t.Fatal("compact provenance changed native lowering")
					}
					if !slices.Equal(ordinary.Imports, compact.Imports) || !slices.Equal(ordinary.MemoryImports(), compact.MemoryImports()) || !reflect.DeepEqual(ordinary.GlobalImports, compact.GlobalImports) {
						t.Fatal("compact provenance changed ordered import metadata", compact.Imports, compact.MemoryImports(), compact.GlobalImports)
					}

					a, err := NewSharedMemory(1, 2)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = a.Close() })
					b, err := NewSharedMemory(2, 3)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = b.Close() })
					bias := NewGlobalI32(7, false)
					t.Cleanup(func() { _ = bias.Close() })
					calls := 0
					imports := NewImports().
						Function("env", "host", func(v int32) int32 { calls++; return v + 5 }).
						Memory("env", "a", a).
						Global("env", "bias", bias).
						Memory("env", "b", b)
					in := rv64MMInstance(t, compact, imports)
					rv64MMCall(t, in, "run", []uint64{24}, 0xdeadbeef00000009)
					if calls != 1 {
						t.Fatal("compact imported host call count", calls)
					}
					if swar {
						rv64MMCall(t, in, "vector", []uint64{0x0123456789abcdef, 0xfedcba9876543210}, 0x0123456789abcdef, 0xfedcba9876543210)
					}
				})
			}
		}
	}
}

func TestRV64PublicCompactImportsFeatureAndDecodeGates(t *testing.T) {
	featureGate := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, []byte{
			1,
			3, 'e', 'n', 'v', 0, 0x7f, 1,
			4, 'h', 'o', 's', 't', byte(wasm.ExternFunc), 0,
		}),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))),
	)
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.MultiMemory = false
	compiled, err := compileWithFrontendFeatures(cfg, featureGate, features)
	if compiled != nil {
		_ = compiled.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "compact imports") {
		t.Fatal("disabled compact-import feature accepted", err)
	}

	malformed := wasmtest.Module(wasmtest.Section(2, []byte{
		1,
		1, 'm', 0, 0x7f, 2,
	}))
	compiled, err = Compile(nil, malformed)
	if compiled != nil {
		_ = compiled.Close()
	}
	if err == nil {
		t.Fatal("malformed compact group published a product")
	}
}
