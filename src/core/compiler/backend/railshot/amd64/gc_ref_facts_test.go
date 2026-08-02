//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func exactGCRefFactModule(t *testing.T, controlBoundary bool) *wasm.Module {
	t.Helper()
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, // struct.new_default 0
		0x21, 0x00, // local.set 0
	}
	if controlBoundary {
		body = append(body, 0x02, 0x40, 0x0b) // empty block conservatively clears facts
	}
	body = append(body,
		0x20, 0x00,
		0xfb, 0x16, 0x00, // ref.cast (ref 0)
		0xd1, // ref.is_null => 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode exact-ref-fact module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate exact-ref-fact module: %v", err)
	}
	return m
}

func TestExactGCReferenceFactsElideProvenCast(t *testing.T) {
	compile := func(m *wasm.Module) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats}); err != nil {
			t.Fatalf("compile exact-ref-fact module: %v", err)
		}
		return stats.Funcs[0]
	}

	saved := exactGCRefFactsEnabled
	defer func() { exactGCRefFactsEnabled = saved }()

	exactGCRefFactsEnabled = true
	on := compile(exactGCRefFactModule(t, false))
	if got := on.Peephole["gc-ref-cast-elide"]; got != 1 {
		t.Fatalf("gc-ref-cast-elide = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := on.Calls["gcnative"]; got != 0 {
		t.Fatalf("native cast calls = %d, want 0", got)
	}

	exactGCRefFactsEnabled = false
	off := compile(exactGCRefFactModule(t, false))
	if got := off.Peephole["gc-ref-cast-elide"]; got != 0 {
		t.Fatalf("disabled gc-ref-cast-elide = %d, want 0", got)
	}
	if got := off.Calls["gcnative"]; got != 1 {
		t.Fatalf("disabled native cast calls = %d, want 1", got)
	}

	exactGCRefFactsEnabled = true
	boundary := compile(exactGCRefFactModule(t, true))
	if got := boundary.Peephole["gc-ref-cast-elide"]; got != 0 {
		t.Fatalf("fact crossed control boundary: gc-ref-cast-elide = %d", got)
	}
	if got := boundary.Calls["gcnative"]; got != 1 {
		t.Fatalf("control-boundary native cast calls = %d, want 1", got)
	}
}
