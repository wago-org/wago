//go:build !tinygo && (amd64 || arm64)

package wago

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"testing"
)

func TestSmallCompileDefersCodeMapping(t *testing.T) {
	cfg := NewRuntimeConfig().WithFunctionWorkers(1).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.codeCache != nil && len(c.codeCache.mem) != 0 {
		t.Fatal("small compile allocated a code mapping before instantiation")
	}
	if _, err := c.MarshalBinary(); err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f", I32(10))
	if err != nil || len(got) != 1 || got[0] != 11 {
		t.Fatalf("result=%v err=%v", got, err)
	}
}
func TestDarwinLargeCompileDefersCodeMapping(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin MAP_JIT policy")
	}
	body := append([]byte{0}, bytes.Repeat([]byte{1}, 128<<10)...)
	body = append(body, 0x41, 7, 0x0b)
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(entry)))
	c, err := NewRuntimeConfig().WithFunctionWorkers(1).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.codeCache != nil && len(c.codeCache.mem) != 0 {
		t.Fatal("Darwin compile-only path acquired a MAP_JIT image")
	}
	if _, err := c.MarshalBinary(); err != nil {
		t.Fatal(err)
	}
}
