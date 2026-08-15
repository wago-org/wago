package standalone

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestMainSourceBakesInvokeExport(t *testing.T) {
	source := string(mainSource(nil, nil, "fib", 3, false, 4, map[string]bool{"inline": false}, false))
	if !strings.Contains(source, `Invoke: "fib", Core: 3, DeferBoundsChecks: false, FunctionWorkers: 4`) ||
		!strings.Contains(source, `"inline": false`) ||
		!strings.Contains(source, `standalone.Run(module, pluginSet(), options, os.Args)`) {
		t.Fatalf("generated main does not invoke fib:\n%s", source)
	}
}

func TestMainSourceEmbedsPrecompiledArtifactForTinyGo(t *testing.T) {
	source := string(mainSource(nil, nil, "", 2, true, 0, nil, true))
	if !strings.Contains(source, "//go:embed module.wago") ||
		!strings.Contains(source, `standalone.RunArtifact(module, pluginSet(), options, os.Args)`) ||
		strings.Contains(source, "module.wasm") {
		t.Fatalf("generated TinyGo main does not load the precompiled artifact:\n%s", source)
	}
	compiler := string(artifactCompilerSource(nil, nil, "", 2, true, 0, nil))
	if !strings.Contains(compiler, `standalone.CompileArtifact(module, pluginSet(), options)`) ||
		!strings.Contains(compiler, `os.WriteFile("module.wago", artifact, 0o644)`) {
		t.Fatalf("generated artifact compiler does not precompile the module:\n%s", compiler)
	}
}

func TestBuildRejectsNonNativeTinyGoTarget(t *testing.T) {
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: "linux", Arch: "amd64"}
	if target == (Target{OS: runtime.GOOS, Arch: runtime.GOARCH}) {
		target = Target{OS: "linux", Arch: "arm64"}
	}
	_, err := Build(Request{Input: input, Target: target, TinyGo: true})
	if err == nil || !strings.Contains(err.Error(), "TinyGo precompiled standalone builds require the native target") {
		t.Fatalf("build error = %v", err)
	}
}

func TestBuildTinyGoEmbedsArtifactWithoutCompiler(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsTinyGo() {
		t.Skipf("TinyGo standalone is unsupported on %s", host)
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	output := filepath.Join(project, "hello")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	result, err := Build(Request{Input: input, Output: output, Target: host, TinyGo: true, KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(result.Output).CombinedOutput(); err != nil {
		t.Fatalf("run TinyGo standalone: %v\n%s", err, output)
	}
	names, err := exec.Command("go", "tool", "nm", result.Output).Output()
	if err != nil {
		t.Fatalf("inspect TinyGo standalone: %v", err)
	}
	for _, forbidden := range []string{
		"compiler/backend/railshot/amd64.", "compiler/backend/railshot/arm64.",
		"compileWithConfig", "CompileModuleWith", "CompileArtifact",
	} {
		if strings.Contains(string(names), forbidden) {
			t.Errorf("TinyGo precompiled executable retains compiler symbol containing %q", forbidden)
		}
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

func TestBuildRejectsCore3OnUnsupportedTarget(t *testing.T) {
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, tailCallStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(Request{Input: input, Target: Target{OS: "windows", Arch: "amd64"}, Core: 3})
	if err == nil || err.Error() != "WebAssembly Core 3 is not supported for target windows/amd64" {
		t.Fatalf("build error = %v", err)
	}
}

func tailCallStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 3, 2, 0, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		10, 9, 2, 2, 0, 0x0b, 4, 0, 0x12, 0, 0x0b,
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
