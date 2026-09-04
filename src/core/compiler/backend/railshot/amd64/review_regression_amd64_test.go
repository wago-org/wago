//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSerialLocalScratchCapacityUsesTotalLocalCountAMD64(t *testing.T) {
	params := make([]wasm.ValType, 96)
	for i := range params {
		params[i] = wasm.I64
	}
	m := modFuncs(t,
		funcDef{body: []byte{0x00, 0x0b}},
		funcDef{params: params, body: []byte{0x01, 0x07, 0x7e, 0x0b}},
		funcDef{params: params[:16], body: []byte{0x00, 0x0b}},
	)
	hints, _, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serialLocalScratchCapacity(hints, inlineTargetTable{}, make([]bool, len(m.Code))), 103; got != want {
		t.Fatalf("serial local scratch capacity = %d, want total parameter-plus-local count %d", got, want)
	}
}

func TestTaglessExceptionHandlingOmitsIntervalSidecarsAMD64(t *testing.T) {
	body := []byte{0x01, 0x80, 0x01, 0x7f} // 128 i32 locals.
	body = append(body, make([]byte, 128)...)
	body = append(body, 0x1f, 0x40, 0x01, byte(wasm.CatchAll), 0x00, 0x0b, 0x0b)
	m := modFuncs(t, funcDef{body: body})
	hints, sidecar, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	view := sidecar.view(hints[0])
	if !hints[0].flags.has(hintModuleEH) {
		t.Fatal("tagless try_table module was not classified as exception handling")
	}
	if hints[0].flags.has(hintIntervalRegionStorage) || len(view.localLastGet) != 0 || len(view.localScore) != 64 {
		t.Fatalf("tagless EH interval sidecars = interval:%v scores:%d last-gets:%d, want false/64/0", hints[0].flags.has(hintIntervalRegionStorage), len(view.localScore), len(view.localLastGet))
	}
}
