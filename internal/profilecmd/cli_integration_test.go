//go:build wago_profile && (darwin || linux)

package profilecmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCLICollectorReexecutionAndJSONReports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WAGO_HOME", filepath.Join(dir, "home"))
	binary := filepath.Join(dir, "wago")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-tags=wago_profile", "-o", binary, "./cli/wago")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	module := filepath.Join(dir, "input with spaces.wasm")
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
	if err := os.WriteFile(module, data, 0600); err != nil {
		t.Fatal(err)
	}
	// A protocol stub, not a real sampling test: it executes the actual CLI child
	// and saves an empty processed profile. Recursion or a lost "profile" prefix
	// makes the real child fail instead of producing a successful manifest.
	collector := filepath.Join(dir, "samply-stub")
	script := `#!/bin/sh
set -eu
if [ "$1" = "--version" ]; then echo test-collector; exit 0; fi
output=''
while [ "$#" -gt 0 ]; do
 case "$1" in
 --output) output="$2"; shift 2 ;;
 --) shift; break ;;
 *) shift ;;
 esac
done
"$@"
cp "$WAGO_PENDING_CAPTURE/manifest.json" "$WAGO_PENDING_SNAPSHOT"
printf '%s' '{"threads":[]}' | gzip -c > "$output"
`
	if err := os.WriteFile(collector, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		return output
	}
	out := filepath.Join(dir, "capture with spaces")
	pendingSnapshot := filepath.Join(dir, "pending.json")
	t.Setenv("WAGO_PENDING_CAPTURE", out)
	t.Setenv("WAGO_PENDING_SNAPSHOT", pendingSnapshot)
	run("profile", "record", "--backend=samply", "--samply="+collector, "--module="+module, "--export=run", "--want=7", "--iterations=1", "--warmup=0", "--out="+out, "--source-maps")
	pendingRaw, err := os.ReadFile(pendingSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var pending struct {
		Complete         bool `json:"complete"`
		CollectorPending bool `json:"collector_pending"`
	}
	if err := json.Unmarshal(pendingRaw, &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Complete || !pending.CollectorPending {
		t.Fatalf("child advertised collector completion: complete=%v pending=%v", pending.Complete, pending.CollectorPending)
	}
	raw, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest profcapture.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.Complete || manifest.CollectorPending || manifest.Iterations != 1 || manifest.Backend != "samply" {
		t.Fatal(manifest)
	}
	annotation := run("profile", "annotate", out, "--function=run", "--json")
	var result struct {
		Functions []functionAnnotation `json:"functions"`
	}
	if err := json.Unmarshal(annotation, &result); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, annotation)
	}
	if len(result.Functions) != 1 || result.Functions[0].Function.Name != "run" {
		t.Fatal(result)
	}

	// The manager normally consumes the first interrupt for its context-aware
	// management commands. A profiler invocation must still honor that signal.
	interruptedOut := filepath.Join(dir, "interrupted")
	child := exec.Command(binary, "profile", "record", "--module="+module, "--export=run", "--want=7", "--duration=60s", "--warmup=0", "--out="+interruptedOut)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(interruptedOut); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("capture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted capture reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("manager swallowed profiler interrupt")
	}
	help := string(run("profile", "record", "--help"))
	if !strings.Contains(help, "--source-maps") || !strings.Contains(help, "--stack-bytes") || strings.Contains(help, "--control") || strings.Contains(help, "--jit-dir") {
		t.Fatal(help)
	}
}
