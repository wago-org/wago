//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func deadLocalAcrossCallModule(t *testing.T, readBeforeOverwrite bool) *wasm.Module {
	t.Helper()
	i32 := wasm.I32
	body := []byte{
		0x00,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // local0 = local0 + 1 (dirty)
		0x10, 0x01, // call the empty local function
	}
	if readBeforeOverwrite {
		body = append(body, 0x20, 0x00, 0x1a) // local.get 0; drop
	}
	body = append(body,
		0x41, 0x07, 0x21, 0x00, // overwrite local0 without needing its old value
		0x20, 0x00, 0x0b,
	)
	return modFuncs(t,
		funcDef{params: []wasm.ValType{i32}, results: []wasm.ValType{i32}, body: body},
		funcDef{body: []byte{0x00, 0x0b}},
	)
}

func TestCallNextUseSkipsOnlyDeadLocalStore(t *testing.T) {
	saved := callNextUseEnabled
	savedInline := inlineEnabled
	defer func() {
		callNextUseEnabled = saved
		inlineEnabled = savedInline
	}()
	callNextUseEnabled = true
	inlineEnabled = false

	dead := deadLocalAcrossCallModule(t, false)
	if got := compileWithStats(t, dead, false).Funcs[0].Peephole["call-dead-local-store"]; got != 1 {
		t.Fatalf("dead call-local stores = %d, want 1", got)
	}
	if got := runAmd64(t, dead, 3); got != 7 {
		t.Fatalf("dead-store result = %d, want 7", got)
	}

	live := deadLocalAcrossCallModule(t, true)
	if got := compileWithStats(t, live, false).Funcs[0].Peephole["call-dead-local-store"]; got != 0 {
		t.Fatalf("live call-local stores skipped = %d, want 0", got)
	}
	if got := runAmd64(t, live, 3); got != 7 {
		t.Fatalf("live-store result = %d, want 7", got)
	}

	callNextUseEnabled = false
	if got := compileWithStats(t, dead, false).Funcs[0].Peephole["call-dead-local-store"]; got != 0 {
		t.Fatalf("disabled call-local stores skipped = %d, want 0", got)
	}
}
