package standalone

import (
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

func TestMainSourceEmbedsPrecompiledArtifact(t *testing.T) {
	source := string(mainSource(nil, nil, "", 2, true, 0, nil, true))
	if !strings.Contains(source, "//go:embed module.wago") ||
		!strings.Contains(source, `standalone.RunArtifact(module, pluginSet(), options, os.Args)`) ||
		strings.Contains(source, "module.wasm") {
		t.Fatalf("generated main does not load the precompiled artifact:\n%s", source)
	}
	compiler := string(artifactCompilerSource(nil, nil, "", 2, true, 0, nil))
	if !strings.Contains(compiler, `standalone.CompileArtifact(module, pluginSet(), options)`) ||
		!strings.Contains(compiler, `os.WriteFile("module.wago", artifact, 0o644)`) {
		t.Fatalf("generated artifact compiler does not precompile the module:\n%s", compiler)
	}
}

func TestBuildRejectsNonNativeTarget(t *testing.T) {
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: "linux", Arch: "amd64"}
	if target == (Target{OS: runtime.GOOS, Arch: runtime.GOARCH}) {
		target = Target{OS: "linux", Arch: "arm64"}
	}
	_, err := Build(Request{Input: input, Target: target})
	if err == nil || !strings.Contains(err.Error(), "precompiled standalone builds require the native target") {
		t.Fatalf("build error = %v", err)
	}
}

func TestBuildEmbedsArtifactWithoutCompiler(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(root, "tests", "fixtures", "wasm", "fib.wasm")
	output := filepath.Join(project, "hello")
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	t.Setenv("GOOS", "windows")
	if host.Arch == "amd64" {
		t.Setenv("GOARCH", "arm64")
	} else {
		t.Setenv("GOARCH", "amd64")
	}
	result, err := Build(Request{Input: input, Output: output, Target: host, Invoke: "fib", KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(result.Output, "20")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run standalone: %v\n%s", err, output)
	} else if got := strings.TrimSpace(string(output)); got != "6765" {
		t.Fatalf("standalone output = %q, want 6765", got)
	}
	assertNoCompilerSymbols(t, result.Output, "standalone")
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
	t.Setenv("GOOS", "windows")
	if host.Arch == "amd64" {
		t.Setenv("GOARCH", "arm64")
	} else {
		t.Setenv("GOARCH", "amd64")
	}
	result, err := Build(Request{Input: input, Output: output, Target: host, TinyGo: true, KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(result.Output).CombinedOutput(); err != nil {
		t.Fatalf("run TinyGo standalone: %v\n%s", err, output)
	}
	assertNoCompilerSymbols(t, result.Output, "TinyGo standalone")
}

func assertNoCompilerSymbols(t *testing.T, executable, description string) {
	t.Helper()
	names, err := exec.Command("go", "tool", "nm", executable).Output()
	if err != nil {
		t.Fatalf("inspect %s: %v", description, err)
	}
	for _, forbidden := range []string{
		"compiler/backend/railshot/amd64.", "compiler/backend/railshot/arm64.", "CompileArtifact",
	} {
		if strings.Contains(string(names), forbidden) {
			var matches []string
			for _, line := range strings.Split(string(names), "\n") {
				if strings.Contains(line, forbidden) {
					matches = append(matches, line)
					if len(matches) == 5 {
						break
					}
				}
			}
			t.Errorf("%s retains compiler symbol containing %q:\n%s", description, forbidden, strings.Join(matches, "\n"))
		}
	}
}

func TestBuildTinyGoStripsByDefault(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsTinyGo() {
		t.Skipf("TinyGo standalone is unsupported on %s", host)
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed")
	}
	if _, err := exec.LookPath("strip"); err != nil {
		t.Skip("strip is not installed")
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
	result, err := Build(Request{Input: input, Output: output, Target: host, TinyGo: true})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(result.Output).CombinedOutput(); err != nil {
		t.Fatalf("run stripped TinyGo standalone: %v\n%s", err, output)
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
