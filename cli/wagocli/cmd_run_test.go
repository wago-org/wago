package wagocli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
)

func TestAutoHostsDoesNotOverrideRuntimeImports(t *testing.T) {
	provided := wago.Imports{
		"plugin.host": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {}),
	}
	hosts := autoHosts(&wago.Compiled{Imports: []string{
		"plugin.host",
		"env.fallback",
	}}, false, provided)
	if _, ok := hosts["plugin.host"]; ok {
		t.Fatal("auto host replaced a runtime-provided plugin import")
	}
	if _, ok := hosts["env.fallback"]; !ok {
		t.Fatal("auto host omitted an unprovided import")
	}
	traced := autoHosts(&wago.Compiled{Imports: []string{"env.trace"}}, true, nil)
	fn, ok := traced["env.trace"].(wago.HostFunc)
	if !ok {
		t.Fatalf("traced host type = %T", traced["env.trace"])
	}
	fn(nil, []uint64{wago.I32(4)}, nil)
}

func TestLoadModuleAndResolveExport(t *testing.T) {
	// (module (func (export "f") (result i32) i32.const 7))
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 5, 1, 1, 'f', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b}
	path := filepath.Join(t.TempDir(), "f.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	rt := wago.NewRuntime()
	defer rt.Close()
	mod := mustLoadModule(path, rt)
	if got := mustResolveExport(mod.Compiled(), ""); got != "f" {
		t.Fatalf("default export = %q", got)
	}
	if got := mustResolveExport(mod.Compiled(), "f"); got != "f" {
		t.Fatalf("named export = %q", got)
	}
	encoded, err := mod.Compiled().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	compiledPath := filepath.Join(t.TempDir(), "f.wago")
	if err := os.WriteFile(compiledPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mustResolveExport(mustLoadModule(compiledPath, rt).Compiled(), "f"); got != "f" {
		t.Fatalf("loaded export = %q", got)
	}
}

func TestRunExecValueMode(t *testing.T) {
	t.Setenv("WAGO_BARE", "1") // exercise the CLI execution path without project/global plugin handoff.
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 5, 1, 1, 'f', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b}
	path := filepath.Join(t.TempDir(), "f.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	runExec(&Ctx{Args: []string{path}, strs: map[string]string{}, bools: map[string]bool{}})
	runExec(&Ctx{Args: []string{path}, strs: map[string]string{"bounds": "all"}, bools: map[string]bool{}})
}

func TestRunExecProgramMode(t *testing.T) {
	t.Setenv("WAGO_BARE", "1")
	// (module (func (export "_start")))
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b}
	path := filepath.Join(t.TempDir(), "start.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	runExec(&Ctx{Args: []string{path, "guest-arg"}, strs: map[string]string{}, bools: map[string]bool{}})
}

func TestRunValueParsingAndFormatting(t *testing.T) {
	cases := []struct {
		in   string
		typ  wago.ValType
		want string
	}{
		{"-2", wago.ValI32, "-2"},
		{"0xffffffff", wago.ValI32, "-1"},
		{"-3", wago.ValI64, "-3"},
		{"0xffffffffffffffff", wago.ValI64, "-1"},
		{"1.5", wago.ValF32, "1.5"},
		{"2.25", wago.ValF64, "2.25"},
	}
	for _, tc := range cases {
		bits, err := parseVal(tc.in, tc.typ)
		if err != nil {
			t.Errorf("parseVal(%q, %s): %v", tc.in, tc.typ, err)
			continue
		}
		if got := fmtVal(bits, tc.typ); got != tc.want {
			t.Errorf("fmtVal(parseVal(%q, %s)) = %q, want %q", tc.in, tc.typ, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in  string
		typ wago.ValType
	}{{"not-a-number", wago.ValI32}, {"not-a-number", wago.ValI64}, {"nope", wago.ValF32}, {"nope", wago.ValF64}} {
		if _, err := parseVal(tc.in, tc.typ); err == nil {
			t.Errorf("parseVal(%q, %s) accepted invalid value", tc.in, tc.typ)
		}
	}
	args := mustParseArgs([]string{"7", "1.5:f32"}, []wago.ValType{wago.ValI32, wago.ValI64})
	if got := format("f", args, []uint64{wago.I64(9)}, []wago.ValType{wago.ValI32, wago.ValF32}, []wago.ValType{wago.ValI64}); got != "f(7, 1.5) = 9" {
		t.Fatalf("format result = %q", got)
	}
	if got := format("g", nil, nil, nil, nil); got != "g() = ()" {
		t.Fatalf("format void = %q", got)
	}
	if got := trapReason(&wago.TrapError{Code: wago.TrapDivZero}); got != "integer division by zero" {
		t.Fatalf("typed trap reason = %q", got)
	}
	if got := trapReason(errors.New("plain error")); got != "plain error" {
		t.Fatalf("plain trap reason = %q", got)
	}
}
