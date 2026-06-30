package main

import (
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestUsageDocumentsCommandSurface(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	text := string(b)
	for _, want := range []string{
		"run <file> [args...]",
		"test <file>",
		"build <file> [-o out]",
		"validate <file>",
		"override per-arg with a suffix",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage text missing %q:\n%s", want, text)
		}
	}
}

func TestIsTestName(t *testing.T) {
	for _, n := range []string{"test_add", "testAdd", "TestThing", "TESTx"} {
		if !isTestName(n) {
			t.Errorf("isTestName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"add", "_test", "helper"} {
		if isTestName(n) {
			t.Errorf("isTestName(%q) = true, want false", n)
		}
	}
}

// testSuiteModule exports three test* functions: a passing one, one returning 0,
// and one that traps; plus a non-test export.
func testSuiteModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("test_pass", 0, 0),
			wasmtest.ExportEntry("test_fail", 0, 1),
			wasmtest.ExportEntry("test_trap", 0, 2),
			wasmtest.ExportEntry("helper", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0x0b}), // i32.const 1
			wasmtest.Code([]byte{0x41, 0x00, 0x0b}), // i32.const 0
			wasmtest.Code([]byte{0x00, 0x0b}),       // unreachable
			wasmtest.Code([]byte{0x41, 0x01, 0x0b}),
		)),
	)
}

func TestRunOneTest(t *testing.T) {
	c, err := wago.Compile(testSuiteModule())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		pass     bool
		contains string
	}{
		{"test_pass", true, ""},
		{"test_fail", false, "returned 0"},
		{"test_trap", false, "trap:"},
	}
	for _, tc := range cases {
		r := runOneTest(c, tc.name)
		if r.pass != tc.pass || !strings.Contains(r.reason, tc.contains) {
			t.Errorf("runOneTest(%q) = {pass:%v reason:%q}, want pass:%v reason~%q",
				tc.name, r.pass, r.reason, tc.pass, tc.contains)
		}
	}
}
