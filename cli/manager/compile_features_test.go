package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	compilecmd "github.com/wago-org/wago/cli/manager/commands/compile"
)

func TestCompileUsesConfiguredFeatures(t *testing.T) {
	if os.Getenv("WAGO_TEST_COMPILE_FEATURES") == "1" {
		commandEnvironment{}.Compile(compilecmd.Options{
			Input: "simd.wasm", Output: "program", Bare: true,
			Core: os.Getenv("WAGO_TEST_CORE"),
		})
		return
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_TEST_COMPILE_FEATURES", "1")
	for _, test := range []struct {
		name, global, local, core string
		wantDisabled              bool
	}{
		{name: "defaults"},
		{name: "global disable", global: `{"version":1,"features":{"simd":false}}`, wantDisabled: true},
		{name: "local disable", local: `{"settings":{"features":{"simd":false}}}`, wantDisabled: true},
		{name: "local enable", global: `{"version":1,"features":{"simd":false}}`, local: `{"settings":{"features":{"simd":true}}}`},
		{name: "explicit core overrides disable", local: `{"settings":{"features":{"simd":false}}}`, core: "2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "settings.json")
			t.Setenv("WAGO_CONFIG", config)
			t.Setenv("WAGO_TEST_CORE", test.core)
			for name, data := range map[string][]byte{
				"simd.wasm":     compileSIMDModule(),
				"settings.json": []byte(test.global),
				"wago.json":     []byte(test.local),
			} {
				if len(data) != 0 {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			command := exec.Command(executable, "-test.run=^TestCompileUsesConfiguredFeatures$")
			command.Dir = dir
			output, err := command.CombinedOutput()
			program := filepath.Join(dir, "program")
			if test.wantDisabled {
				if err == nil || !strings.Contains(string(output), "simd disabled") {
					t.Fatalf("compile with SIMD disabled: error=%v\n%s", err, output)
				}
				if _, err := os.Stat(program); !os.IsNotExist(err) {
					t.Fatalf("disabled module produced an executable: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("compile with SIMD enabled: %v\n%s", err, output)
			}
			output, err = exec.Command(program).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != "7" {
				t.Fatalf("standalone execution: error=%v, output=%q", err, output)
			}
		})
	}
}

// (module (func (export "main") (result i32)
//
//	v128.const i32x4 0 0 0 0 drop i32.const 7))
func compileSIMDModule() []byte {
	return []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 8, 1, 4, 'm', 'a', 'i', 'n', 0, 0,
		10, 25, 1, 23, 0, 0xfd, 0x0c,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0x1a, 0x41, 7, 0x0b,
	}
}
