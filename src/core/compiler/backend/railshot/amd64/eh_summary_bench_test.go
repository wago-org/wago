//go:build amd64

package amd64

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
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
	return m
}

var exceptionSummaryBenchSink []nativeabi.FunctionRootMap

func BenchmarkExceptionModuleSummary(b *testing.B) {
	benchmarkExceptionModuleSummary(b, exceptionSummaryBenchModule(1000))
}

func BenchmarkExceptionModuleSummaryEarlyTag(b *testing.B) {
	for _, imports := range []int{0, 100, 10000} {
		b.Run(fmt.Sprint(imports), func(b *testing.B) {
			m := exceptionSummaryBenchModule(1)
			for i := 0; i < imports; i++ {
				m.Imports = append(m.Imports, wasm.Import{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 1})})
			}
			benchmarkExceptionModuleSummary(b, m)
		})
	}
}

func benchmarkExceptionModuleSummary(b *testing.B, m *wasm.Module) {
	b.Helper()
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
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.FuncRef}
	m.Types[0].SubTypes = append(m.Types[0].SubTypes,
		wasm.SubType{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef, wasm.AnyRef}}},
		wasm.SubType{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef, wasm.FuncRef, wasm.AnyRef}}},
	)
	m.Imports = []wasm.Import{
		{Type: wasm.NewTagExternType(wasm.TagType{})},
		{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 1})},
		{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32})},
		{Type: wasm.NewTagExternType(wasm.TagType{Type: wasm.TypeIdx{Index: 2}})},
		{Type: wasm.NewMemExternType(wasm.MemType{})},
	}
	m.Tags = []wasm.TagType{{Type: wasm.TypeIdx{Index: 3}}}
	locals := []int{0, 1, 5}
	roots := []int{1, 2, 1}
	for i := range m.Code {
		m.Code[i].Locals = wasm.Locals{Runs: []wasm.LocalRun{{Count: uint32(locals[i]), Type: wasm.I64}}}
		if roots[i] == 2 {
			body := m.Code[i].BodyBytes
			m.Code[i].BodyBytes = append(append([]byte(nil), body[:len(body)-1]...), body...)
		}
	}
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
		want := nativeabi.FunctionRootMap{
			LocalFunction: uint32(i),
			FrameBytes:    uint32(frameHdrBytes + 8*locals[i] + (ehRecordSlots+roots[i]*ehRootSlots)*8),
		}
		for root := 0; root < roots[i]; root++ {
			base := firstRootPayloadOffset + 8*locals[i] + root*ehRootSlots*8
			for lane := 0; lane < 2; lane++ {
				want.Slots = append(want.Slots, nativeabi.RootSlot{Offset: uint32(base + lane*8), Kind: nativeabi.RootFuncRef})
			}
			for lane := shared.EHGCLaneBase; lane < shared.EHPayloadLanes; lane++ {
				want.Slots = append(want.Slots, nativeabi.RootSlot{Offset: uint32(base + lane*8), Kind: nativeabi.RootGCRef})
			}
		}
		if !reflect.DeepEqual(rm, want) {
			t.Fatalf("map %d: got %#v, want %#v", i, rm, want)
		}
	}
}

func TestExceptionSummaryRejectsImportedDefinedOwnershipConflict(t *testing.T) {
	for _, params := range [][]wasm.ValType{{wasm.I32, wasm.FuncRef}, {wasm.FuncRef, wasm.I32}} {
		m := exceptionSummaryBenchModule(1)
		m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{params[0]}
		m.Types[0].SubTypes = append(m.Types[0].SubTypes,
			wasm.SubType{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{params[1]}}},
		)
		m.Tags = []wasm.TagType{{Type: wasm.TypeIdx{Index: 2}}}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if _, err := BuildExceptionRootMaps(m); err == nil || !strings.Contains(err.Error(), "mixes scalar and reference ownership") {
			t.Fatalf("imported/defined ownership %v: got %v", params, err)
		}
	}
}
