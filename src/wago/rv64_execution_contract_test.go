//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PortableRootedModule() []byte {
	callee := []byte{0, 0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}
	caller := []byte{1, 1, 0x7f, 0x41, 0x29, 0xfb, 0, 0, 0x41, 0x2a, 0xfb, 0, 0, 0x10, 0,
		0xfb, 2, 0, 0, 0x21, 0, 0xfb, 2, 0, 0, 0x20, 0, 0x6a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x64, 0, 1, 0x64, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(callee))), callee...), append(wasmtest.ULEB(uint32(len(caller))), caller...))))
}

func TestRV64StateOnlyExceptionFrameMapIsEmpty(t *testing.T) {
	roots := &compiledGCFrameRoots{}
	c := &Compiled{requiredFeatures: CoreFeatureExceptionHandling, validateMemo: &validateMemo{gcFrameRoots: roots}}
	c.ensureCodeCache()
	c.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
	defer c.Close()
	if !rv64SourceFrameOwnership(c) {
		t.Fatal("state-only empty frame map rejected")
	}
	c.NumImports = 1
	if rv64SourceFrameOwnership(c) {
		t.Fatal("imported execution used state-only frame exception")
	}
	c.NumImports = 0
	roots.safepoints = []compiledGCFrameSafepoint{{id: 1, frameBytes: 48}}
	if rv64SourceFrameOwnership(c) {
		t.Fatal("state-only image acquired a native safepoint")
	}
	roots.safepoints = nil
	c.code = []byte{0x13, 0, 0, 0}
	if rv64SourceFrameOwnership(c) {
		t.Fatal("state-only frame exception admitted native code")
	}
}
