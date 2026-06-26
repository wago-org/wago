package main

import (
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestValidateFileUsesWasm3FrontendForSupportedModule(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("main", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
	)
	path := writeTempWasm(t, mod)
	msg, err := validateFile(path)
	if err != nil {
		t.Fatalf("validateFile: %v", err)
	}
	if !strings.Contains(msg, ": OK (1 funcs, 1 exports)") {
		t.Fatalf("validateFile message = %q", msg)
	}
}

func TestValidateFileRejectsProposalFeatureDecodedByWasm3(t *testing.T) {
	body := []byte{0xfd, 0x0c}
	body = append(body, make([]byte, 16)...)
	body = append(body, 0x1a, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	_, err := validateFile(writeTempWasm(t, mod))
	if err == nil || !strings.Contains(err.Error(), "unsupported instruction V128Const") {
		t.Fatalf("validateFile error = %v, want wasm3 unsupported-instruction rejection", err)
	}
}

func writeTempWasm(t *testing.T, mod []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(mod); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
