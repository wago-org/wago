package standalone

import (
	"bytes"
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

func TestRequireToolchainExplainsPathRequirement(t *testing.T) {
	originalFind := findTool
	t.Cleanup(func() { findTool = originalFind })
	findTool = func(string) (string, error) { return "", exec.ErrNotFound }
	if err := requireToolchain("go"); err == nil || !strings.Contains(err.Error(), `"go" is available on PATH`) || !strings.Contains(err.Error(), "https://go.dev/dl/") {
		t.Fatalf("Go toolchain error = %v", err)
	}
	if err := requireToolchain("tinygo"); err == nil || !strings.Contains(err.Error(), `"tinygo" is available on PATH`) || !strings.Contains(err.Error(), "https://tinygo.org/getting-started/install/") {
		t.Fatalf("TinyGo toolchain error = %v", err)
	}
}

func TestRequireToolchainAcceptsVersionFoundOnPath(t *testing.T) {
	originalFind := findTool
	t.Cleanup(func() { findTool = originalFind })
	findTool = func(name string) (string, error) { return "/tools/" + name, nil }
	for _, name := range []string{"go", "tinygo"} {
		if err := requireToolchain(name); err != nil {
			t.Fatalf("requireToolchain(%q) = %v", name, err)
		}
	}
}

func TestMainSourceBakesInvokeExport(t *testing.T) {
	source := string(mainSource(nil, nil, "fib", 3, false, 4, map[string]bool{"inline": false}, 0, false))
	if !strings.Contains(source, `Invoke: "fib", Core: 3, DeferBoundsChecks: false, FunctionWorkers: 4`) ||
		!strings.Contains(source, `"inline": false`) ||
		!strings.Contains(source, `standalone.Run(module, pluginSet(), options, os.Args)`) {
		t.Fatalf("generated main does not invoke fib:\n%s", source)
	}
}

func TestBuildLinksMachineCodeWithoutCompiler(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(root, "tests", "fixtures", "wasm", "fib.wasm")
	output := filepath.Join(project, DefaultOutput("hello.wasm", host))
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
	assertNoCompilerSymbols(t, result.Output)
	binary, err := os.ReadFile(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	wasm, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(binary, wasm) {
		t.Fatal("standalone embeds the original Wasm instead of only its native image")
	}

	command := exec.Command(result.Output, "20")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run standalone: %v\n%s", err, output)
	} else if got := strings.TrimSpace(string(output)); got != "6765" {
		t.Fatalf("standalone output = %q, want 6765", got)
	}
}

func TestBuildTinyGoLinksMachineCodeWithoutCompiler(t *testing.T) {
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
	assertNoCompilerSymbols(t, result.Output)
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

func TestGeneratedSourcesBakeFeatures(t *testing.T) {
	for _, source := range [][]byte{
		mainSource(nil, nil, "", 0, true, 0, nil, 0, true),
		mainSource(nil, nil, "", 0, true, 0, nil, 0, true),
	} {
		if !strings.Contains(string(source), "Features: 0, FeaturesSet: true") {
			t.Fatalf("generated source lost explicit empty feature mask:\n%s", source)
		}
	}
}

func TestLinuxRISCV64StandaloneTarget(t *testing.T) {
	target, err := ParseTarget("linux/riscv64")
	if err != nil {
		t.Fatal(err)
	}
	if !target.supportsCore3() {
		t.Fatal("riscv64 Core 3 standalone is disabled")
	}
	if target.supportsTinyGo() {
		t.Fatal("riscv64 TinyGo standalone is enabled")
	}
	for _, goos := range []string{"darwin", "windows"} {
		if _, err := ParseTarget(goos + "/riscv64"); err == nil {
			t.Fatalf("accepted %s/riscv64", goos)
		}
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
	if err == nil || !strings.Contains(err.Error(), "standalone builds require the native target") {
		t.Fatalf("build error = %v", err)
	}
}

func assertNoCompilerSymbols(t *testing.T, executable string) {
	t.Helper()
	names, err := exec.Command("go", "tool", "nm", executable).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect standalone symbols: %v\n%s", err, names)
	}
	for _, forbidden := range []string{"compiler/backend/railshot/amd64.", "compiler/backend/railshot/arm64.", "compiler/backend/railshot/riscv64.Compile", "compiler/wasm.DecodeModule", "compiler/wasm.decodeModule", "compiler/frontend.Lower", "standalonegen."} {
		if strings.Contains(string(names), forbidden) {
			for _, line := range strings.Split(string(names), "\n") {
				if strings.Contains(line, forbidden) {
					t.Errorf("standalone retains compiler: %s", line)
					break
				}
			}
		}
	}
}

func TestBuildNativeModuleWithData(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	dir := t.TempDir()
	input := filepath.Join(dir, "data.wasm")
	// (module (memory 1) (data (i32.const 0) "M")
	//   (func (export "run") (result i32) (i32.load8_u (i32.const 0))))
	source := []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 96, 0, 1, 127, 3, 2, 1, 0, 5, 3, 1, 0, 1, 7, 7, 1, 3, 114, 117, 110, 0, 0, 10, 9, 1, 7, 0, 65, 0, 45, 0, 0, 11, 11, 7, 1, 0, 65, 0, 11, 1, 77}
	if err := os.WriteFile(input, source, 0644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	result, err := Build(Request{Input: input, Output: filepath.Join(dir, DefaultOutput("program.wasm", target)), Target: target, Invoke: "run", Core: 2, KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(result.Output).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "77" {
		t.Fatalf("native data initialization: %s %v", output, err)
	}
	assertNoCompilerSymbols(t, result.Output)
}

func TestBuildNativeModuleWithGCReferences(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsCore3() || host.Arch == "riscv64" {
		t.Skip("requires general native GC support")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	output := filepath.Join(t.TempDir(), "gc")
	result, err := Build(Request{Input: filepath.Join(root, "tests/fixtures/wasm/gc-allocation-mix.wasm"), Output: output, Target: host, Core: 3, Invoke: "structs", KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNoCompilerSymbols(t, result.Output)
	got, err := exec.Command(result.Output, "1000").CombinedOutput()
	if err != nil || strings.TrimSpace(string(got)) != "499500" {
		t.Fatalf("GC standalone: %s, %v", got, err)
	}
}
