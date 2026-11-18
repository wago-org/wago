//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile && wago_codegenstats

package amd64

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamLoopNativeExport(t *testing.T) {
	dir := os.Getenv("WAGO_STREAM_EXPORT")
	if dir == "" {
		t.Skip("explicit native export directory required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var tc streamingControlCase
	for _, x := range streamingControlCases() {
		if x.name == "loop-local-joins" {
			tc = x
		}
	}
	var body []byte
	for _, x := range tc.body {
		if x == 0x45 {
			body = append(body, 0x41, 0, 0x46)
		} else {
			body = append(body, x)
		}
	}
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	artifact := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(tc.params, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(entry)))
	if err := os.WriteFile(filepath.Join(dir, "loop.wasm"), artifact, 0644); err != nil {
		t.Fatal(err)
	}
	ah := sha256.Sum256(artifact)
	m, err := wasm.DecodeModule(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Mode, InputSHA256, NativeSHA256                                  string
		Bytes, Entry, InternalEntry, FrameBytes, MaxSpillSlots, Selected int
		Result                                                           uint64
	}
	var rows []row
	old := streamingOperandsEnabled
	defer func() { streamingOperandsEnabled = old }()
	for _, on := range []bool{false, true} {
		streamingOperandsEnabled = on
		name := "off"
		if on {
			name = "on"
		}
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, DeferCodeMapping: true, Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		got := runCompiledAmd64u(t, cm, tc.args...)
		if got != tc.want {
			t.Fatalf("%s got%d want%d", name, got, tc.want)
		}
		selected := stats.Funcs[0].Peephole["streaming-function"]
		want := 0
		if on {
			want = 1
		}
		if selected != want {
			t.Fatalf("selection%s=%d", name, selected)
		}
		h := sha256.Sum256(cm.Code)
		if err = os.WriteFile(filepath.Join(dir, name+".bin"), cm.Code, 0644); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row{name, hex.EncodeToString(ah[:]), hex.EncodeToString(h[:]), len(cm.Code), cm.Entry[0], cm.InternalEntry[0], stats.Funcs[0].FrameBytes, stats.Funcs[0].MaxSpillSlots, selected, got})
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "receipt.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log("OFF/ON exact input verified, outputs correct, native differences allowed; export not timing")
}
