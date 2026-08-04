package standalone

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTargetAndDefaultOutput(t *testing.T) {
	target, err := ParseTarget("windows/arm64")
	if err != nil || target != (Target{OS: "windows", Arch: "arm64"}) {
		t.Fatalf("ParseTarget = %#v, %v", target, err)
	}
	if got := DefaultOutput("hello.wasm", target); got != "hello.exe" {
		t.Fatalf("windows output = %q", got)
	}
	if _, err := ParseTarget("plan9/riscv64"); err == nil {
		t.Fatal("unsupported target was accepted")
	}
}

func TestBuildSelectsOnlyTargetBackend(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")

	for _, test := range []struct {
		arch    string
		machine elf.Machine
		backend string
		other   string
	}{
		{arch: "amd64", machine: elf.EM_X86_64, backend: "railshot/amd64", other: "railshot/arm64"},
		{arch: "arm64", machine: elf.EM_AARCH64, backend: "railshot/arm64", other: "railshot/amd64"},
	} {
		output := filepath.Join(project, "hello-"+test.arch)
		result, err := Build(Request{
			Input: input, Output: output, Target: Target{OS: "linux", Arch: test.arch}, KeepSymbols: true,
		})
		if err != nil {
			t.Fatalf("build linux/%s: %v", test.arch, err)
		}
		binary, err := elf.Open(result.Output)
		if err != nil {
			t.Fatalf("open linux/%s: %v", test.arch, err)
		}
		if binary.Machine != test.machine {
			t.Errorf("linux/%s machine = %v, want %v", test.arch, binary.Machine, test.machine)
		}
		_ = binary.Close()
		names, err := exec.Command("go", "tool", "nm", result.Output).Output()
		if err != nil {
			t.Fatalf("nm linux/%s: %v", test.arch, err)
		}
		if !strings.Contains(string(names), test.backend) {
			t.Errorf("linux/%s executable does not contain target backend %q", test.arch, test.backend)
		}
		if strings.Contains(string(names), test.other) {
			t.Errorf("linux/%s executable contains opposite backend %q", test.arch, test.other)
		}
	}
}

func emptyStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
}
