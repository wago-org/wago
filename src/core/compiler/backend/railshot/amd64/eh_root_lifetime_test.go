//go:build amd64

package amd64

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func exceptionFuncrefRootLifetimeModule(payloadType []byte) []byte {
	tagType := append([]byte{0x60, 0x01}, payloadType...)
	tagType = append(tagType, 0x00)
	indexedNullableResult := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	indexedNullableExnResult := []byte{0x60, 0x00, 0x02, 0x63, 0x00, 0x64, byte(wasm.HeapExn)}
	throwBody := []byte{0xd2, 0x00, 0x08, 0x00, 0x0b}
	catchBody := []byte{
		0x02, 0x03, // block type 3: (result (ref null 0) (ref exn))
		0x1f, 0x40, 0x01, byte(wasm.CatchRef), 0x00, 0x00,
		0x10, 0x01, // call thrower
		0x0b, // end try_table
		0x00, // normal fallthrough is unreachable
		0x0b, // end result block
		0x1a, // drop rooted exn identity; keep funcref payload
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			tagType,
			indexedNullableResult,
			indexedNullableExnResult,
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00}, []byte{0x02})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code(throwBody),
			wasmtest.Code(catchBody),
		)),
	)
}

func TestExceptionFuncrefRootsInitializeAndClear(t *testing.T) {
	if got := unsafe.Sizeof(storage{}); got != 32 {
		t.Fatalf("storage size = %d, want unchanged 32 bytes", got)
	}
	m, err := wasm.DecodeModule(exceptionFuncrefRootLifetimeModule([]byte{0x64, 0x00}))
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[2].Peephole["eh-root-init"]; got != 1 {
		t.Fatalf("EH root initializations = %d, want 1", got)
	}
	if got := stats.Funcs[2].Peephole["eh-root-clear"]; got != 1 {
		t.Fatalf("EH root clears = %d, want 1", got)
	}
}

func TestExceptionPayloadBackendRejectsNonFunctionReference(t *testing.T) {
	m, err := wasm.DecodeModule(exceptionFuncrefRootLifetimeModule([]byte{0x6f}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileModuleWith(m, CompileOptions{}); err == nil || !strings.Contains(err.Error(), "scalar or non-null indexed-function") {
		t.Fatalf("externref EH payload compile = %v, want strict backend rejection", err)
	}
}
