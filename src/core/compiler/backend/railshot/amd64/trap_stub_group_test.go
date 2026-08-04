//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func manyInlinedTrapFunctionsModule(tb testing.TB, n int) *wasm.Module {
	tb.Helper()
	caller := []byte{0x00}
	for i := 0; i < n; i++ {
		caller = append(caller, 0x20, 0x00, 0x10)
		caller = append(caller, wasmtest.ULEB(uint32(i+1))...)
		caller = append(caller, 0x1a)
	}
	caller = append(caller, 0x41, 0x00, 0x0b)

	funcs := make([][]byte, n+1)
	codes := make([][]byte, n+1)
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(0)
	}
	codes[0] = append(wasmtest.ULEB(uint32(len(caller))), caller...)
	for i := 1; i <= n; i++ {
		// 1 / local.get 0 has a divide-by-zero trap site and remains small enough
		// to inline into the caller.
		body := []byte{0x00, 0x41, 0x01, 0x20, 0x00, 0x6e, 0x0b}
		codes[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatalf("decode: %v", err)
	}
	return m
}

func repeatedInlinedTrapFunctionModule(tb testing.TB, n int) *wasm.Module {
	tb.Helper()
	caller := []byte{0x00}
	for i := 0; i < n; i++ {
		caller = append(caller, 0x20, 0x00, 0x10, 0x01, 0x1a)
	}
	caller = append(caller, 0x41, 0x00, 0x0b)
	helper := []byte{0x00, 0x41, 0x01, 0x20, 0x00, 0x6e, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(helper))), helper...),
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatalf("decode: %v", err)
	}
	return m
}

func TestSortTrapSitesByFunction(t *testing.T) {
	sites := []trapSite{
		{branch: 10, function: 7, pc: 1},
		{branch: 20, function: 2, pc: 2},
		{branch: 30, function: 7, pc: 3},
		{branch: 40, function: 1, pc: 4},
		{branch: 50, function: 2, pc: 5},
	}
	sortTrapSitesByFunction(sites)
	seen := map[int]bool{}
	for i, site := range sites {
		if i != 0 && sites[i-1].function > site.function {
			t.Fatalf("sites are not sorted at %d: %+v", i, sites)
		}
		seen[site.branch] = true
	}
	for _, branch := range []int{10, 20, 30, 40, 50} {
		if !seen[branch] {
			t.Fatalf("site branch %d was lost during sort: %+v", branch, sites)
		}
	}
}

func TestManyInlinedTrapFunctionsCompile(t *testing.T) {
	const n = 64
	m := manyInlinedTrapFunctionsModule(t, n)
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Calls["inline"]; got != n {
		t.Fatalf("inlined calls = %d, want %d", got, n)
	}
	if got := runAmd64(t, m, 1); got != 0 {
		t.Fatalf("result = %d, want 0", got)
	}
}

func BenchmarkManyInlinedTrapFunctions(b *testing.B) {
	m := manyInlinedTrapFunctionsModule(b, 1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CompileModule(m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRepeatedInlinedTrapSites(b *testing.B) {
	m := repeatedInlinedTrapFunctionModule(b, 16*1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CompileModule(m); err != nil {
			b.Fatal(err)
		}
	}
}
