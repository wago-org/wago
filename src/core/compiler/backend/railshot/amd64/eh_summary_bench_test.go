//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"testing"
)

func exceptionSummaryBenchModule(n int) *wasm.Module {
	body := []byte{0x02, 0x69, 0x1f, 0x40, 0x01, byte(wasm.CatchAllRef), 0x00, 0x01, 0x0b, 0xd0, 0x69, 0x0b, 0x1a, 0x0b}
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}}}, {Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}, Imports: make([]wasm.Import, n), Code: make([]wasm.Func, n), FuncTypes: make([]wasm.TypeIdx, n)}
	for i := range m.Imports {
		m.Imports[i].Type = wasm.NewTagExternType(wasm.TagType{})
	}
	for i := range m.Code {
		m.Code[i].BodyBytes = body
		m.FuncTypes[i] = wasm.TypeIdx{Index: 1}
	}
	_ = m.TagCount()
	return m
}

var exceptionSummaryBenchSink []nativeabi.FunctionRootMap

func BenchmarkExceptionModuleSummary(b *testing.B) {
	m := exceptionSummaryBenchModule(1000)
	if err := wasm.ValidateModule(m); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		exceptionSummaryBenchSink, err = BuildExceptionRootMaps(m)
		if err != nil {
			b.Fatal(err)
		}
	}
}
func TestExceptionSummaryMultipleFunctions(t *testing.T) {
	m := exceptionSummaryBenchModule(3)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 3 {
		t.Fatalf("maps=%d", len(maps))
	}
	for i, rm := range maps {
		if rm.LocalFunction != uint32(i) || len(rm.Slots) != 8 {
			t.Fatalf("map %d: %#v", i, rm)
		}
	}
}
