package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestDecodeValidateAcceptsSupportedMVPModule(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("main", 0, 0),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))),
	)
	m, err := DecodeValidate(mod)
	if err != nil {
		t.Fatalf("DecodeValidate supported MVP module: %v", err)
	}
	if len(m.Code) != 1 || len(m.Exports) != 2 {
		t.Fatalf("module summary = %d funcs, %d exports", len(m.Code), len(m.Exports))
	}
}

func TestRejectUnsupportedGlobalTypes(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "vec", wasm.V128, false))))
	_, err := DecodeValidate(mod)
	assertErrContains(t, err, "unsupported global type v128 at import 0")
}

func TestRejectUnsupportedImports(t *testing.T) {
	memImport := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memImport = append(memImport, 0x02, 0x00, 0x01) // memory import, min 1 page
	mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(memImport)))
	_, err := DecodeValidate(mod)
	assertErrContains(t, err, "unsupported import memory at import 0")
}

func TestRejectUnsupportedReferenceTableAndTagForms(t *testing.T) {
	t.Run("externref table", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x00, 0x01})))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported table element type externref at table 0")
	})
	t.Run("tag section", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported tag section at tag section")
	})
}

func TestRejectUnsupportedProposalFeaturesDecodedByWasm3(t *testing.T) {
	t.Run("memory64", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x04, 0x00}))) // memory64 min 0
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported memory memory64 at memory 0")
	})
	t.Run("simd instruction", func(t *testing.T) {
		body := []byte{0xfd, 0x0c}
		body = append(body, make([]byte, 16)...)
		body = append(body, 0x1a, 0x0b) // v128.const 0; drop; end
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported instruction V128Const at function 0 instruction 0")
	})
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}
