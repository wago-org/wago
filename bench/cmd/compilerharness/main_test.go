package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
)

func TestExpandArgs(t *testing.T) {
	got, err := expandArgs([]string{"compile", "-o", "{artifact}", "prefix={wasm}"}, "/in.wasm", "/out.bin")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"compile", "-o", "/out.bin", "prefix=/in.wasm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	if _, err := expandArgs([]string{"{wasm}"}, "in", "out"); err == nil {
		t.Fatal("missing artifact placeholder accepted")
	}
}

func TestReadConfigIsStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	valid := `{"version":2,"engines":[{"name":"cranelift","command":"wasmtime","args":["compile","-o","{artifact}","{wasm}"],"version_args":["--version"]}]}`
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(valid[:len(valid)-1]+`,"unknown":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err == nil {
		t.Fatal("unknown config field accepted")
	}
}

func TestReadConfigAcceptsOnlyKnownBuiltinCompilers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	valid := `{"version":2,"engines":[{"name":"dragline","builtin":"dragline","target":"native","workers":8,"required":true}]}`
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err != nil {
		t.Fatal(err)
	}
	railshot := `{"version":2,"engines":[{"name":"railshot","builtin":"railshot","target":"native","workers":8,"required":true}]}`
	if err := os.WriteFile(path, []byte(railshot), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"version":2,"engines":[{"name":"dragline","builtin":"unknown"}]}`,
		`{"version":2,"engines":[{"name":"dragline","builtin":"dragline","target":"other"}]}`,
		`{"version":2,"engines":[{"name":"dragline","builtin":"dragline","command":"wago"}]}`,
		`{"version":2,"engines":[{"name":"dragline","builtin":"dragline","workers":-1}]}`,
		`{"version":2,"engines":[{"name":"external","command":"wasmtime","target":"native","args":["{wasm}","{artifact}"]}]}`,
		`{"version":2,"engines":[{"name":"external","command":"wasmtime","workers":2,"args":["{wasm}","{artifact}"]}]}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readConfig(path); err == nil {
			t.Fatalf("invalid config accepted: %s", invalid)
		}
	}
}

func TestBuiltinDraglineCompilerWritesLoadableArtifact(t *testing.T) {
	wasmPath := filepath.Join(t.TempDir(), "input.wasm")
	artifactPath := filepath.Join(t.TempDir(), "output.wago")
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmPath, module, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runBuiltinCompiler("dragline", "compat", 1, wasmPath, artifactPath); err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runBuiltinCompiler("dragline", "invalid", 1, wasmPath, artifactPath); err == nil {
		t.Fatal("invalid builtin target accepted")
	}
	if err := runBuiltinCompiler("dragline", "compat", -1, wasmPath, artifactPath); err == nil {
		t.Fatal("negative builtin worker count accepted")
	}
}

func TestBuiltinRailshotCompilerWritesLoadableArtifact(t *testing.T) {
	wasmPath := filepath.Join(t.TempDir(), "input.wasm")
	artifactPath := filepath.Join(t.TempDir(), "output.wago")
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmPath, module, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runBuiltinCompiler("railshot", "native", 1, wasmPath, artifactPath); err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRunReportRecordsChildCPUTime(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestCompilerHarnessCPUHelper$")
	cmd.Env = append(os.Environ(), "WAGO_COMPILER_HARNESS_CPU_HELPER=1")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	row := newRunReport(3, 1, "test", 17*time.Millisecond, cmd.ProcessState, 42, 24)
	if row.Round != 3 || row.Order != 1 || row.Engine != "test" || row.WallNanos != int64(17*time.Millisecond) || row.ArtifactBytes != 42 || row.NativeCodeBytes != 24 {
		t.Fatalf("unexpected run identity: %+v", row)
	}
	if row.CPUUserNanos < 0 || row.CPUSystemNanos < 0 || row.CPUTotalNanos != row.CPUUserNanos+row.CPUSystemNanos {
		t.Fatalf("invalid CPU accounting: %+v", row)
	}
}

func TestCompilerHarnessCPUHelper(t *testing.T) {
	if os.Getenv("WAGO_COMPILER_HARNESS_CPU_HELPER") != "1" {
		return
	}
	var sum uint64
	for i := uint64(0); i < 1_000_000; i++ {
		sum += i
	}
	if sum == 0 {
		t.Fatal("unreachable")
	}
}
