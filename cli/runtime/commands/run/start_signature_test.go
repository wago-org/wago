package run

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStartSignature(t *testing.T) {
	const helper = "WAGO_TEST_START_SIGNATURE"
	if path := os.Getenv(helper); path != "" {
		implementation{environment: testEnvironment{}}.Run(command.NewContext([]string{path, "guest-arg"}, nil, nil))
		os.Exit(0)
	}
	for _, tc := range []struct {
		name, export    string
		params, results []wasm.ValType
		body            []byte
		want            string
		fail            bool
	}{
		{name: "void", export: "_start", body: []byte{0x0b}},
		{name: "result", export: "_start", results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 7, 0x0b}, want: "_start must have signature () -> ()", fail: true},
		{name: "parameter", export: "_start", params: []wasm.ValType{wasm.I32}, body: []byte{0x0b}, want: "_start must have signature () -> ()", fail: true},
		{name: "normal", export: "value", results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 7, 0x0b}, want: "7\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(tc.params, tc.results))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry(tc.export, 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))),
			)
			path := filepath.Join(t.TempDir(), "module.wasm")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestRunStartSignature$", "-test.count=1")
			cmd.Env = append(os.Environ(), helper+"="+path, "WAGO_BARE=1", "WAGO_HOME="+t.TempDir())
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.fail || strings.Contains(string(output), "panic:") || !strings.Contains(string(output), tc.want) {
				t.Fatalf("run=%v, output=%q", err, output)
			}
		})
	}
}
