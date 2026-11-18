//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// gcLocalTypeFactModule stores a precisely typed call result in an anyref
// local. The declared local type cannot prove the later cast. Only value flow
// from the call result can prove it.
func gcLocalTypeFactModule(t *testing.T, copyLocal, controlBoundary bool) *wasm.Module {
	t.Helper()
	types := wasmtest.Vec(
		[]byte{0x50, 0, 0x5f, 0},    // open base struct 0
		[]byte{0x50, 1, 0, 0x5f, 0}, // open child struct 1 <: 0
		[]byte{0x60, 0, 1, 0x64, 1}, // () -> (ref 1)
		wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
	)
	producer := wasmtest.Code([]byte{0xfb, 0, 1, 0x0b}) // struct.new 1
	body := []byte{0x01, 0x01, 0x6e}                    // one anyref local
	if copyLocal {
		body = []byte{0x01, 0x02, 0x6e} // two anyref locals
	}
	body = append(body, 0x10, 0, 0x21, 0) // call producer; local.set 0
	if copyLocal {
		body = append(body, 0x20, 0, 0x21, 1) // local 1 = local 0
	}
	if controlBoundary {
		body = append(body, 0x02, 0x40, 0x0b) // empty block: conservative join
	}
	local := byte(0)
	if copyLocal {
		local = 1
	}
	body = append(body,
		0x20, local,
		0xfb, 22, 0, // ref.cast (ref 0)
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(10, wasmtest.Vec(
			producer,
			append(wasmtest.ULEB(uint32(len(body))), body...),
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCLocalTypeFactElidesCallResultCast(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name            string
		copyLocal       bool
		controlBoundary bool
		disabled        bool
		want            int
	}{
		{name: "direct", want: 1},
		{name: "local-copy", copyLocal: true, want: 1},
		{name: "control-join-invalidates", controlBoundary: true},
		{name: "value-facts-disabled", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(gcLocalTypeFactModule(t, tc.copyLocal, tc.controlBoundary), CompileOptions{
				GCStructHelpers: true,
				Stats:           &stats,
				Optimizations: map[string]bool{
					"inline":      false,
					"value-facts": !tc.disabled,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[1].Peephole["gc-local-type-cast-elide"]; got != tc.want {
				t.Fatalf("local-flow cast elisions=%d want%d", got, tc.want)
			}
		})
	}
}

func gcLocalTypeFactProducerModule(t *testing.T, priorCast, overwrite bool) *wasm.Module {
	t.Helper()
	params := []wasm.ValType(nil)
	if priorCast || overwrite {
		params = []wasm.ValType{wasm.AnyRef}
	}
	types := wasmtest.Vec(
		[]byte{0x50, 0, 0x5f, 0},    // open base struct 0
		[]byte{0x50, 1, 0, 0x5f, 0}, // open child struct 1 <: 0
		wasmtest.FuncType(params, []wasm.ValType{wasm.AnyRef}),
	)
	body := []byte{0x01, 0x01, 0x6e} // one anyref local
	if priorCast {
		body = append(body,
			0x20, 0,
			0xfb, 22, 1, // checked ref.cast (ref 1)
			0x21, 1,
			0x20, 1,
			0xfb, 22, 0, // proved ref.cast (ref 0)
		)
	} else {
		body = append(body,
			0xfb, 0, 1, // struct.new 1
			0x21, byte(len(params)),
		)
		if overwrite {
			body = append(body, 0x20, 0, 0x21, byte(len(params)))
		}
		body = append(body,
			0x20, byte(len(params)),
			0xfb, 22, 0, // ref.cast (ref 0)
		)
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCLocalTypeFactProducerAndInvalidation(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name                 string
		priorCast, overwrite bool
		want                 int
	}{
		{name: "constructor", want: 1},
		{name: "successful-cast", priorCast: true, want: 1},
		{name: "unknown-overwrite", overwrite: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(gcLocalTypeFactProducerModule(t, tc.priorCast, tc.overwrite), CompileOptions{
				GCStructHelpers: true,
				Stats:           &stats,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-local-type-cast-elide"]; got != tc.want {
				t.Fatalf("local-flow cast elisions=%d want%d", got, tc.want)
			}
		})
	}
}

func TestGCLocalTypeFactTableBound(t *testing.T) {
	var facts gcLocalTypeFacts
	for i := 0; i < maxGCLocalTypeFacts+1; i++ {
		facts.set(i, gcReferenceTypeFact{typeIndex: uint32(i), flags: gcReferenceFactValid})
	}
	if facts.n != maxGCLocalTypeFacts {
		t.Fatalf("fact count=%d want%d", facts.n, maxGCLocalTypeFacts)
	}
	if _, ok := facts.get(0); ok {
		t.Fatal("oldest fact survived bounded replacement")
	}
	if got, ok := facts.get(maxGCLocalTypeFacts); !ok || got.typeIndex != uint32(maxGCLocalTypeFacts) {
		t.Fatalf("newest fact=%#v,%t", got, ok)
	}
	for i := 1; i <= maxGCLocalTypeFacts; i++ {
		facts.remove(i)
	}
	if facts.n != 0 {
		t.Fatalf("fact count after removal=%d want0", facts.n)
	}
}

func TestGCReferenceTypeFactProofLimits(t *testing.T) {
	m := gcLocalTypeFactProducerModule(t, false, false)
	for _, tc := range []struct {
		name        string
		fact        gcReferenceTypeFact
		sub         uint32
		target      int64
		exact, want bool
	}{
		{name: "same-type", fact: gcReferenceTypeFact{typeIndex: 0, flags: gcReferenceFactValid}, sub: 22, target: 0, want: true},
		{name: "proper-subtype", fact: gcReferenceTypeFact{typeIndex: 1, flags: gcReferenceFactValid}, sub: 22, target: 0, want: true},
		{name: "nullable-target", fact: gcReferenceTypeFact{typeIndex: 1, flags: gcReferenceFactValid | gcReferenceFactNullable}, sub: 23, target: 0, want: true},
		{name: "nullable-source", fact: gcReferenceTypeFact{typeIndex: 1, flags: gcReferenceFactValid | gcReferenceFactNullable}, sub: 22, target: 0},
		{name: "downcast", fact: gcReferenceTypeFact{typeIndex: 0, flags: gcReferenceFactValid}, sub: 22, target: 1},
		{name: "open-exact", fact: gcReferenceTypeFact{typeIndex: 1, flags: gcReferenceFactValid}, sub: 22, target: 1, exact: true},
		{name: "proved-exact", fact: gcReferenceTypeFact{typeIndex: 1, flags: gcReferenceFactValid | gcReferenceFactExact}, sub: 22, target: 1, exact: true, want: true},
		{name: "invalid", sub: 22, target: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fact.provesCast(m, tc.sub, tc.target, tc.exact); got != tc.want {
				t.Fatalf("proof=%t want%t", got, tc.want)
			}
		})
	}
}

func TestGCReferenceTypeFactProofDoesNotAllocate(t *testing.T) {
	m := gcLocalTypeFactProducerModule(t, false, false)
	fact := gcReferenceTypeFact{typeIndex: 1, flags: gcReferenceFactValid}
	if !fact.provesCast(m, 22, 0, false) {
		t.Fatal("proper-subtype fixture did not prove its cast")
	}
	if got := testing.AllocsPerRun(100, func() {
		if !fact.provesCast(m, 22, 0, false) {
			panic("proper-subtype proof changed")
		}
	}); got != 0 {
		t.Fatalf("proper-subtype proof allocations=%v want0", got)
	}
}
