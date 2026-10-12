//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
)

const threadsSpecRevision = "cc535ada1aa21cfaa3cabf3ac73b89acef78a0a0"

// This tests the proposal's atomic instruction script. Its concurrent scripts
// require a separate shared-module thread runner and are not replayed here.
func TestThreadsOfficialAtomicSuite(t *testing.T) {
	checkout := os.Getenv("WAGO_THREADS_SPEC_DIR")
	if checkout == "" {
		t.Skip("set WAGO_THREADS_SPEC_DIR to the pinned WebAssembly/threads checkout")
	}
	revision, err := exec.Command("git", "-C", checkout, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(revision)) != threadsSpecRevision {
		t.Fatalf("threads suite revision = %s, %v; want %s", revision, err, threadsSpecRevision)
	}
	tool, err := resolveWast2JSON()
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(tool, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(version)) != "1.0.41" {
		t.Fatalf("wast2json version = %s, %v; want 1.0.41", version, err)
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "atomic.json")
	source := filepath.Join(checkout, "test", "core", "threads", "atomic.wast")
	if out, err := exec.Command(tool, "--enable-all", source, "-o", output).CombinedOutput(); err != nil {
		t.Fatalf("convert official atomic script: %v: %s", err, out)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var script specExecFile
	if err := json.Unmarshal(raw, &script); err != nil {
		t.Fatal(err)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3 | wago.CoreFeatureThreads).
		WithBoundsChecks(wago.BoundsChecksExplicit)
	stats := runSpecExecFileWithConfig(t, "threads/atomic", dir, script, cfg)
	t.Logf("official atomic: modules(pass=%d fail=%d skip=%d) assertions(pass=%d fail=%d skip=%d) gaps(%s)",
		stats.modulesPassed, stats.modulesFailed, stats.modulesSkipped,
		stats.assertionsPassed, stats.assertionsFailed, stats.assertionsSkipped, stats.gapSummary())
	if stats.modulesPassed == 0 || stats.assertionsPassed == 0 || stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
		t.Fatal("official atomic script did not pass completely")
	}
}

func TestThreadsOfficialConcurrentScripts(t *testing.T) {
	checkout := os.Getenv("WAGO_THREADS_SPEC_DIR")
	if checkout == "" {
		t.Skip("set WAGO_THREADS_SPEC_DIR to the pinned WebAssembly/threads checkout")
	}
	revision, err := exec.Command("git", "-C", checkout, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(revision)) != threadsSpecRevision {
		t.Fatalf("threads suite revision = %s, %v", revision, err)
	}
	interpreter := filepath.Join(checkout, "interpreter", "wasm")
	version, err := exec.Command(interpreter, "-v", "--help").CombinedOutput()
	if err != nil || firstLine(version) != "wasm 2.0.1 reference interpreter" {
		t.Fatalf("threads interpreter identity = %s, %v", firstLine(version), err)
	}
	converter, err := resolveRepoFile("scripts/spec-interpreter-json.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"simple", "thread", "nested", "deeply_nested", "unlinkable", "wait_notify", "LB", "LB_atomic", "MP", "MP_atomic", "SB", "SB_atomic"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, name+".bin.wast")
			output := filepath.Join(dir, name+".json")
			if out, err := exec.Command(interpreter, "-d", filepath.Join(checkout, "test", "core", "threads", name+".wast"), "-o", binary).CombinedOutput(); err != nil {
				t.Fatalf("threads text conversion: %v: %s", err, out)
			}
			if out, err := exec.Command(converter, binary, output, "--source", "WebAssembly/threads interpreter 2.0.1").CombinedOutput(); err != nil {
				t.Fatalf("threads JSON conversion: %v: %s", err, out)
			}
			raw, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var script specExecFile
			if err := json.Unmarshal(raw, &script); err != nil {
				t.Fatal(err)
			}
			cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3 | wago.CoreFeatureThreads).WithBoundsChecks(wago.BoundsChecksExplicit)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stats := runSpecExecFileShared(t, "threads/"+name, dir, script, cfg, nil, nil, &specThreadScope{ctx: ctx, cancel: cancel})
			t.Logf("modules(pass=%d fail=%d skip=%d) assertions(pass=%d fail=%d skip=%d) gaps(%s)", stats.modulesPassed, stats.modulesFailed, stats.modulesSkipped, stats.assertionsPassed, stats.assertionsFailed, stats.assertionsSkipped, stats.gapSummary())
			if stats.modulesPassed == 0 || stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
				t.Fatal("official concurrent script did not pass completely")
			}
		})
	}
}
