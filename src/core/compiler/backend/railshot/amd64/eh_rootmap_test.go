//go:build amd64

package amd64

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func exceptionRootMapModule(catches int) []byte {
	indexedFuncParam := []byte{0x60, 0x01, 0x64, 0x00, 0x00} // (func (param (ref 0)))
	body := []byte{0x02, 0x40, 0x1f, 0x40}
	body = append(body, wasmtest.ULEB(uint32(catches))...)
	for i := 0; i < catches; i++ {
		body = append(body, byte(wasm.CatchRef), 0x00, 0x00)
	}
	body = append(body, 0x01, 0x0b, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), indexedFuncParam)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func catchAllRootMapModule(tagParams ...wasm.ValType) []byte {
	types := [][]byte{wasmtest.FuncType(nil, nil)}
	tags := make([][]byte, len(tagParams))
	for i, param := range tagParams {
		types = append(types, wasmtest.FuncType([]wasm.ValType{param}, nil))
		tags[i] = []byte{0x00, byte(i + 1)}
	}
	body := []byte{0x02, 0x40, 0x1f, 0x40, 0x01, byte(wasm.CatchAllRef), 0x00, 0x01, 0x0b, 0x0b, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec(tags...)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// A module needs no tag directory to catch an exception from an imported
// function. Its catch_all_ref slot must still root every dedicated GC lane.
func TestBuildExceptionRootMapsCatchAllWithoutTags(t *testing.T) {
	body := []byte{0x02, 0x69, 0x1f, 0x40, 0x01, byte(wasm.CatchAllRef), 0x00,
		0x10, 0x00, 0x0b, 0x00, 0x0b, 0x1a, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("foreign"), wasmtest.Name("throw")...), 0x00, 0x00))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if m.TagCount() != 0 {
		t.Fatal("fixture must have no declared or imported tags")
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0].LocalFunction != 0 || maps[0].FrameBytes != uint32(frameHdrBytes+(ehRecordSlots+ehRootSlots)*8) || len(maps[0].Slots) != shared.MaxEHTagPayloadWords {
		t.Fatalf("tag-free catch_all_ref root maps = %#v", maps)
	}
	for i, got := range maps[0].Slots {
		want := uint32(firstRootPayloadOffset + (shared.EHGCLaneBase+i)*8)
		if got.Offset != want || got.Kind != nativeabi.RootGCRef {
			t.Fatalf("root slot %d = %#v, want offset %d/gc", i, got, want)
		}
	}
}

func TestBuildExceptionRootMapsSingleFuncrefPayload(t *testing.T) {
	m, err := wasm.DecodeModule(exceptionRootMapModule(1))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0].LocalFunction != 0 || maps[0].FrameBytes != uint32(frameHdrBytes+(ehRecordSlots+ehRootSlots)*8) || len(maps[0].Slots) != 1 {
		t.Fatalf("exception root maps = %#v", maps)
	}
	if got := maps[0].Slots[0]; got.Offset != uint32(firstRootPayloadOffset) || got.Kind != nativeabi.RootFuncRef {
		t.Fatalf("funcref root slot = %#v, want the first root payload slot/funcref", got)
	}
	if err := nativeabi.ValidateRootMaps(maps, len(m.Code)); err != nil {
		t.Fatalf("collector-facing validation: %v", err)
	}
}

func TestBuildExceptionRootMapsCatchAllUsesModuleTagOwnership(t *testing.T) {
	m, err := wasm.DecodeModule(catchAllRootMapModule(wasm.FuncRef))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || len(maps[0].Slots) != 1+shared.MaxEHTagPayloadWords {
		t.Fatalf("catch_all_ref root maps = %#v", maps)
	}
	if got := maps[0].Slots[0]; got.Offset != uint32(firstRootPayloadOffset) || got.Kind != nativeabi.RootFuncRef {
		t.Fatalf("catch_all_ref funcref root slot = %#v, want the first root payload slot/funcref", got)
	}
}

func TestBuildExceptionRootMapsRejectsCatchAllMixedOwnership(t *testing.T) {
	for _, params := range [][]wasm.ValType{{wasm.FuncRef, wasm.ExternRef}, {wasm.FuncRef, wasm.I64}} {
		m, err := wasm.DecodeModule(catchAllRootMapModule(params...))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildExceptionRootMaps(m); err == nil || !strings.Contains(err.Error(), "mixes") {
			t.Fatalf("catch_all_ref mixed ownership %v = %v, want strict rejection", params, err)
		}
	}
}

// GC payload words live in dedicated lanes, so a catch_all_ref slot over tags
// that disagree on which parameter is a collector reference is still exact:
// the GC lanes are scanned, never the i64 or externref word in lane 0.
func TestBuildExceptionRootMapsCatchAllGCLanes(t *testing.T) {
	m, err := wasm.DecodeModule(catchAllRootMapModule(wasm.I64, wasm.AnyRef, wasm.ExternRef))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || len(maps[0].Slots) != shared.MaxEHTagPayloadWords {
		t.Fatalf("catch_all_ref GC root maps = %#v", maps)
	}
	for i, got := range maps[0].Slots {
		want := uint32(firstRootPayloadOffset + (shared.EHGCLaneBase+i)*8)
		if got.Offset != want || got.Kind != nativeabi.RootGCRef {
			t.Fatalf("catch_all_ref root slot %d = %#v, want offset %d/gc", i, got, want)
		}
	}
}

// Root records are sized per function, so a fifth catch_ref gets a fifth
// slot instead of the former fixed-four rejection.
func TestBuildExceptionRootMapsSizesRootsPerFunction(t *testing.T) {
	m, err := wasm.DecodeModule(exceptionRootMapModule(5))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || len(maps[0].Slots) != 5 || maps[0].FrameBytes != uint32(frameHdrBytes+(ehRecordSlots+5*ehRootSlots)*8) {
		t.Fatalf("five-root map = %#v", maps)
	}
	for i, slot := range maps[0].Slots {
		if want := uint32(firstRootPayloadOffset + i*ehRootSlots*8); slot.Offset != want {
			t.Fatalf("root %d payload offset = %d, want %d", i, slot.Offset, want)
		}
	}
}

func TestBuildExceptionRootMapsExplicitReferenceBlockTypeReproducers(t *testing.T) {
	for _, tc := range []struct {
		typeIndex uint32
		bytes     int
		sha256    string
	}{
		{992, 2032, "a05cd2a4c9415eb4942683fb801440692436b624a670351fe7cb6149d035e085"},
		{6, 53, "5497f8ec1d509b28e74894b011d9f2c59780999ecf8b9bad654fdb6bba1514b2"},
	} {
		t.Run(fmt.Sprint(tc.typeIndex), func(t *testing.T) {
			data := wasmtest.ExplicitReferenceBlockTypeModule(tc.typeIndex)
			if got := fmt.Sprintf("%x", sha256.Sum256(data)); len(data) != tc.bytes || got != tc.sha256 {
				t.Fatalf("fixture = %d bytes, sha256 %s; want %d bytes, %s", len(data), got, tc.bytes, tc.sha256)
			}
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			maps, err := BuildExceptionRootMaps(m)
			if err != nil {
				t.Fatal(err)
			}
			if len(maps) != 0 {
				t.Fatalf("tag-free fixture has root maps: %#v", maps)
			}
		})
	}
}

func TestBuildExceptionRootMapsTryTableExplicitReferenceBlockType(t *testing.T) {
	const typeIndex = 6
	body := []byte{
		0x1f, 0x64, typeIndex, 0x00, // try_table (result (ref 6)), no catches
		0xfb, 0x00, typeIndex, // struct.new 6
		0x0b, 0x0b, // end try_table, function
	}
	types := make([][]byte, typeIndex+2)
	for i := 0; i <= typeIndex; i++ {
		types[i] = []byte{0x5f, 0x00}
	}
	types[typeIndex+1] = []byte{0x60, 0x00, 0x01, 0x64, typeIndex}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{typeIndex + 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildExceptionRootMaps(m); err != nil {
		t.Fatal(err)
	}
}

// firstRootPayloadOffset is the first payload word of root 0 in a function
// with one try_table level and no locals.
const firstRootPayloadOffset = frameHdrBytes + ehRecordSlots*8 + 8
