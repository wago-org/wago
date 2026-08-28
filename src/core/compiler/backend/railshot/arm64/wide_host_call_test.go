//go:build arm64

package arm64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func wideStructHelperModule(t *testing.T) (*wasm.Module, frontend.GCTypeMetadata) {
	t.Helper()
	const fields = 403
	structType := append([]byte{0x5f}, wasmtest.ULEB(fields)...)
	body := []byte{0x00}
	for i := 0; i < fields; i++ {
		structType = append(structType, 0x7f, 0x00)
		body = append(body, 0x41, 0x00)
	}
	body = append(body, 0xfb, 0x00, 0x00, 0x1a, 0x0b)
	binary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	return m, metadata
}

func TestWideStructHelperUsesU16CheckedHostCapacity(t *testing.T) {
	m, metadata := wideStructHelperModule(t)
	if _, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		SyncHostSlots:   404,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts,
		}},
	}); err != nil {
		t.Fatalf("404-slot helper compile: %v", err)
	}
	if _, err := CompileModuleWith(&wasm.Module{}, CompileOptions{SyncHostSlots: coreruntime.MaxSyncHostSlots + 1}); err == nil {
		t.Fatal("capacity above uint16 was accepted")
	}
}

func TestWideCapacityDoesNotAdmitWidePublicImport(t *testing.T) {
	const fields = 65
	structType := append([]byte{0x5f}, wasmtest.ULEB(fields)...)
	params := make([]wasm.ValType, 65)
	for i := 0; i < fields; i++ {
		structType = append(structType, 0x7f, 0x00)
		params[i] = wasm.I32
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("wide")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	body := []byte{0x00}
	for range params {
		body = append(body, 0x41, 0x00)
	}
	body = append(body, 0x10, 0x00)
	for range params {
		body = append(body, 0x41, 0x00)
	}
	body = append(body, 0xfb, 0x00, 0x00, 0x1a, 0x0b)
	binary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(params, nil), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		SyncHostSlots:   fields + 1,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "at most 64") {
		t.Fatalf("wide public import compile error = %v", err)
	}
}
