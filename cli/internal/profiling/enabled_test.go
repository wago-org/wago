//go:build wago_profile

package profiling

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMain(m *testing.M) {
	// The record command re-executes its executable for supervised capture.
	// Route that child through the same entry point as the production CLI;
	// otherwise the test binary recursively runs the suite instead.
	if Capture(os.Args[1:]) {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestProfileCommandRecordsAndParsesReports(t *testing.T) {
	automation.Configure(automation.Options{})
	t.Cleanup(func() { automation.Configure(automation.Options{}) })
	root := Append(&command.Cmd{Name: "wago"})
	group := root.Child("profile")
	if group == nil || len(group.Children) != 5 || group.Child("capture") != nil {
		t.Fatal("wrong public profile surface")
	}
	var help bytes.Buffer
	group.Child("record").PrintHelp(&help, "wago profile record")
	if !strings.Contains(help.String(), "--source-maps") || !strings.Contains(help.String(), "--stack-bytes") || strings.Contains(help.String(), "--control") || strings.Contains(help.String(), "--jit-dir") {
		t.Fatal(help.String())
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.wasm")
	out := filepath.Join(dir, "capture")
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := group.Child("record")
	ctx, err := cmd.Parse("wago profile record", []string{"--module", path, "--export", "run", "--want", "7", "--iterations", "1", "--warmup", "0", "--out", out, "--source-maps"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(ctx)
	raw, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m profcapture.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if !m.Complete || m.Iterations != 1 || !m.SourceMapsRequested {
		t.Fatal(m)
	}
	// Flags after a positional capture are accepted and retain JSON mode.
	top := group.Child("top")
	ctx, err = top.Parse("wago profile top", []string{out, "--json"})
	if err != nil || !ctx.Bool("json") || len(ctx.Args) != 1 {
		t.Fatal(ctx, err)
	}
}
