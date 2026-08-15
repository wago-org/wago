//go:build linux

package standalone

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTinyGoArtifactSupportsCooperativeInterruption(t *testing.T) {
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := fmt.Sprintf("module tinygo-interruption-test\n\ngo 1.22\n\nrequire github.com/wago-org/wago v0.0.0\n\nreplace github.com/wago-org/wago => %s\n", root)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(interruptionArtifactCompilerSource), 0o644); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if err := runGo(dir, environment, false, "mod", "tidy"); err != nil {
		t.Fatal(err)
	}
	if err := runGo(dir, environment, false, "run", "-tags=wago_target_tinygo", "."); err != nil {
		t.Fatalf("compile artifact for TinyGo target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(interruptionTinyGoRunnerSource), 0o644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "runner")
	// A distinct host thread publishes cancellation while the CPU-bound guest
	// owns its execution thread. This isolates whether the artifact contains the
	// cooperative poll that the TinyGo runtime needs to consume that request.
	if err := runTool("tinygo", dir, environment, false, "build", "-scheduler=threads", "-gc=conservative", "-tags=wago_precompiled", "-o", executable, "."); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, executable).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("TinyGo standalone did not interrupt its infinite guest within 5s: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("TinyGo standalone interruption: %v\n%s", err, output)
	}
}

const interruptionArtifactCompilerSource = `package main

import (
	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/standalone"
)

var infiniteLoop = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x08, 0x01, 0x04, 0x73, 0x70, 0x69, 0x6e, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b,
}

func main() {
	artifact, err := standalone.CompileArtifact(infiniteLoop, wago.PluginSet{}, standalone.Options{})
	if err == nil {
		err = os.WriteFile("module.wago", artifact, 0o644)
	}
	if err != nil {
		panic(err)
	}
}
`

const interruptionTinyGoRunnerSource = `package main

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/wago-org/wago"
)

//go:embed module.wago
var artifact []byte

func main() {
	compiled, err := wago.Load(artifact)
	if err != nil {
		panic(err)
	}
	rt := wago.NewRuntime()
	module, err := rt.AdoptModule(compiled)
	if err != nil {
		panic(err)
	}
	instance, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, invokeErr := instance.InvokeContext(ctx, "spin")
		done <- invokeErr
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			panic(err)
		}
	case <-time.After(2 * time.Second):
		panic("infinite guest did not consume cancellation")
	}
}
`
