package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"github.com/wago-org/wago/testutil/wasmtest"
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

func TestBuildExceptionRootMapsSingleFuncrefPayload(t *testing.T) {
	m, err := wasm.DecodeModule(exceptionRootMapModule(1))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0].LocalFunction != 0 || maps[0].FrameBytes != 336 || len(maps[0].Slots) != 1 {
		t.Fatalf("exception root maps = %#v", maps)
	}
	if got := maps[0].Slots[0]; got.Offset != 248 || got.Kind != nativeabi.RootFuncRef {
		t.Fatalf("funcref root slot = %#v, want offset 248/funcref", got)
	}
	if err := nativeabi.ValidateRootMaps(maps, len(m.Code)); err != nil {
		t.Fatalf("collector-facing validation: %v", err)
	}
}

func TestBuildExceptionRootMapsRejectsFifthFixedRoot(t *testing.T) {
	m, err := wasm.DecodeModule(exceptionRootMapModule(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildExceptionRootMaps(m); err == nil || !strings.Contains(err.Error(), "exceeds 4 fixed roots") {
		t.Fatalf("five-root map = %v, want bounded rejection", err)
	}
}
