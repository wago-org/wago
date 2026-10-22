//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func rv64RootedTableDirectorySource(count int) []byte {
	tables := make([][]byte, count)
	for i := range tables {
		tables[i] = []byte{0x6e, 1, 0, 0}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(4, wasmtest.Vec(tables...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 42, 0x0b}))),
	)
}

func TestRV64RootedTableDirectorySourceAndArtifact(t *testing.T) {
	for _, count := range []int{3, 256, 257, maxRV64ExecutionTables} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			c, err := NewRuntimeConfig().Compile(rv64RootedTableDirectorySource(count))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			loaded := publicArtifactRoundTrip(t, c)
			defer loaded.Close()
			for _, candidate := range []*Compiled{c, loaded} {
				if candidate.tableCount() != count || !rv64FreshRootedGC(candidate.executionView()) {
					t.Fatal("rooted table directory receipt", candidate.tableCount())
				}
				in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				result, err := in.Invoke("run")
				in.Close()
				if err != nil || len(result) != 1 || result[0] != 42 {
					t.Fatalf("rooted directory result=%v,%v", result, err)
				}
			}
		})
	}
	if c, err := NewRuntimeConfig().Compile(rv64RootedTableDirectorySource(maxRV64ExecutionTables + 1)); err == nil {
		c.Close()
		t.Fatal("rooted directory exceeded the public metadata bound")
	}
}

func TestRV64RootedTableClearOnlyHelperFrame(t *testing.T) {
	for _, vector := range []bool{false, true} {
		clear := []byte{0x41, 0, 0xd0, 0x6e, 0x26, 0, 0x41, 42, 0x0b}
		if vector {
			prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
			clear = append(append(prefix, 0x1a), clear...)
		}
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, []byte{2, 1, 1}),
			wasmtest.Section(4, []byte{1, 0x6e, 1, 1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(clear), wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 42, 0x0b}))),
		)
		c, err := NewRuntimeConfig().Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for _, candidate := range []*Compiled{c, loaded} {
			in, err := Instantiate(candidate, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("clear")
			in.Close()
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("clear-only vector=%t: %v,%v", vector, got, err)
			}
		}
	}
}
