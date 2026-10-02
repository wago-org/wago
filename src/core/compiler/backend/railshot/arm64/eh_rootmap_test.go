//go:build arm64

package arm64

import (
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

// firstRootPayloadOffset is the first payload word of root 0 in a function
// with one try_table level and no locals.
const firstRootPayloadOffset = frameHdrBytes + ehRecordSlots*8 + 8
