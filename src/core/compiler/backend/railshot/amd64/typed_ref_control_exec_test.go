//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func typedRefControlModule(body []byte, result wasm.ValType) *wasm.Module {
	indexedNullable := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	returnType := result
	if result.Kind == wasm.ValRef {
		returnType = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{indexedNullable}, Results: []wasm.ValType{returnType}}}}},
		},
		FuncTypes: []wasm.TypeIdx{{Index: 1}},
		Code:      []wasm.Func{{BodyBytes: body}},
	}
}

func runTypedRefControl(t *testing.T, m *wasm.Module, ref uint64) (uint64, error) {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(128)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, base, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args := arena.Alloc(8)
	binary.LittleEndian.PutUint64(args, ref)
	results := arena.Alloc(8)
	trap := arena.Alloc(8)
	err = eng.Call(base+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	return binary.LittleEndian.Uint64(results), err
}

func TestTypedRefControlNullBranches(t *testing.T) {
	brOnNull := typedRefControlModule([]byte{
		0x02, 0x40, // block
		0x20, 0x00, // local.get 0
		0xd5, 0x00, // br_on_null 0
		0x1a,             // drop the non-null fallthrough reference
		0x41, 0x02, 0x0f, // return 2
		0x0b,
		0x41, 0x01, // null branch returns 1
		0x0b,
	}, wasm.I32)
	for _, tc := range []struct {
		ref  uint64
		want uint64
	}{{0, 1}, {0x1234, 2}} {
		got, err := runTypedRefControl(t, brOnNull, tc.ref)
		if err != nil || uint32(got) != uint32(tc.want) {
			t.Fatalf("br_on_null(%#x) = %#x, %v; want %d", tc.ref, got, err, tc.want)
		}
	}

	brOnNonNull := typedRefControlModule([]byte{
		0x02, 0x64, 0x00, // block (result (ref type 0))
		0x20, 0x00, // local.get 0
		0xd6, 0x00, // br_on_non_null 0
		0x41, 0x01, 0x0f, // null fallthrough consumes the reference and returns 1
		0x0b,
		0x1a,       // drop the taken branch's non-null reference
		0x41, 0x02, // non-null branch returns 2
		0x0b,
	}, wasm.I32)
	for _, tc := range []struct {
		ref  uint64
		want uint64
	}{{0, 1}, {0x1234, 2}} {
		got, err := runTypedRefControl(t, brOnNonNull, tc.ref)
		if err != nil || uint32(got) != uint32(tc.want) {
			t.Fatalf("br_on_non_null(%#x) = %#x, %v; want %d", tc.ref, got, err, tc.want)
		}
	}
}

func TestTypedRefBrOnNonNullCarriesLabelPrefixOnlyOnFallthrough(t *testing.T) {
	indexedNullable := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	m := &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32, wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0, Rec: true}), false))}}}}},
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, indexedNullable}, Results: []wasm.ValType{wasm.I32}}}}},
		},
		FuncTypes: []wasm.TypeIdx{{Index: 1}},
		Code: []wasm.Func{{BodyBytes: []byte{
			0x02, 0x00, // block (type 0): (i32) -> (i32)
			0x20, 0x00, // local.get prefix
			0x20, 0x01, // local.get reference
			0xd6, 0x00, // br_on_non_null 0
			0x0f, // null fallthrough returns the i32 prefix only
			0x0b,
			0x1a, // taken branch produced the non-null reference
			0x0b,
		}}},
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate br_on_non_null prefix: %v", err)
	}
}

func TestTypedRefAsNonNullPreservesIdentityAndTraps(t *testing.T) {
	indexedResult := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	m := typedRefControlModule([]byte{0x20, 0x00, 0xd4, 0x0b}, indexedResult)
	got, err := runTypedRefControl(t, m, 0x12345678)
	if err != nil || got != 0x12345678 {
		t.Fatalf("ref.as_non_null identity = %#x, %v", got, err)
	}
	if _, err := runTypedRefControl(t, m, 0); err == nil || !strings.Contains(err.Error(), "null reference") {
		t.Fatalf("ref.as_non_null null error = %v", err)
	}
}
