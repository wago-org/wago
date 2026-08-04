package standalone

import (
	"archive/zip"
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
	source := string(mainSource(nil, nil, "fib", 3, false, 4, map[string]bool{"inline": false}))
	if !strings.Contains(source, `Invoke: "fib", Core: 3, DeferBoundsChecks: false, FunctionWorkers: 4`) ||
		!strings.Contains(source, `"inline": false`) ||
		!strings.Contains(source, `standalone.Run(module, pluginConfig, options, os.Args)`) {
		t.Fatalf("generated main does not invoke fib:\n%s", source)
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

func TestBuildRunsNativeCore3WithCommandLinePlugin(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	proxy := t.TempDir()
	writeTestPluginProxy(t, proxy)
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, tailCallStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	output := filepath.Join(project, "hello")
	if target.OS == "windows" {
		output += ".exe"
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	t.Setenv("GOPROXY", "file:///"+strings.TrimLeft(filepath.ToSlash(proxy), "/"))
	t.Setenv("GOSUMDB", "off")
	moduleCache := t.TempDir()
	t.Setenv("GOMODCACHE", moduleCache)
	t.Cleanup(func() {
		_ = filepath.Walk(moduleCache, func(path string, _ os.FileInfo, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
	})

	if _, err := Build(Request{
		Input: input, Output: output, Target: target,
		Core: 3, FunctionWorkers: 2, Plugins: "example.com/standalone-plugin@v0.0.0",
	}); err != nil {
		t.Fatalf("build with command-line plugin: %v", err)
	}
	if output, err := exec.Command(output).CombinedOutput(); err != nil {
		t.Fatalf("run with command-line plugin: %v\n%s", err, output)
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

func writeTestPluginProxy(t *testing.T, proxy string) {
	t.Helper()
	const (
		module  = "example.com/standalone-plugin"
		version = "v0.0.0"
	)
	dir := filepath.Join(proxy, "example.com", "standalone-plugin", "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		version + ".mod":  "module " + module + "\n\ngo 1.22\n\nrequire github.com/wago-org/wago v0.0.0\n",
		version + ".info": `{"Version":"v0.0.0","Time":"2026-01-01T00:00:00Z"}`,
		"list":            version + "\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := os.Create(filepath.Join(dir, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(archive)
	entries := map[string]string{
		"go.mod": files[version+".mod"],
		"register/register.go": `package register

import "github.com/wago-org/wago"

type extension struct{}

func (extension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID: "test.standalone", Repository: "https://example.com/standalone-plugin", License: "Apache-2.0",
	}
}
func (extension) Register(*wago.Registry) error { return nil }

func init() {
	wago.RegisterExtension("example.com/standalone-plugin", func() wago.Extension { return extension{} })
}
`,
	}
	for name, contents := range entries {
		entry, err := zw.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
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
