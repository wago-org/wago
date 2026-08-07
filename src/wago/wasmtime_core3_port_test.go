//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo

package wago_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/tests/regressiontest"
	"github.com/wago-org/wago/tests/wasmtimecore3"
)

const wasmtimeCore3FixtureEnv = "WAGO_WASMTIME_CORE3_FIXTURE"

func TestWasmtimeCore3Corpus(t *testing.T) {
	if fixture := os.Getenv(wasmtimeCore3FixtureEnv); fixture != "" {
		nonce := regressiontest.RequireProtocol(t)
		stats := runWasmtimeCore3FixtureInProcess(t, fixture)
		outcome, err := json.Marshal(regressionOutcomeFromStats(fixture, nonce, stats))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s%s\n", regressionFixtureOutcomeMarker, outcome)
		return
	}
	root := filepath.Clean("../../tests/regressions/wasmtime-core3")
	fixtures, err := wasmtimecore3.LoadManifest(filepath.Join(root, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var total specExecStats
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(strings.TrimSuffix(fixture.Path, ".wast"), func(t *testing.T) {
			dir := filepath.Join(root, "core", strings.TrimSuffix(fixture.Path, ".wast"))
			modules, assertions, err := wasmtimecore3.ValidateFixture(dir)
			if err != nil {
				t.Fatal(err)
			}
			stats := runWasmtimeCore3FixtureChild(t, fixture.Path)
			if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
				t.Fatalf("fixture stats = %+v, want no failures or skips", stats)
			}
			if stats.modulesPassed != modules || stats.assertionsPassed != assertions {
				t.Fatalf("fixture accounting = %d modules, %d assertions; want %d, %d", stats.modulesPassed, stats.assertionsPassed, modules, assertions)
			}
			total.add(stats)
		})
	}
	if total.modulesPassed != 215 || total.assertionsPassed != 690 {
		t.Fatalf("Wasmtime Core 3 totals = %d modules, %d assertions; want 215, 690", total.modulesPassed, total.assertionsPassed)
	}
}

func TestWasmtimeBigMemoryBehaviorSourceEquivalent(t *testing.T) {
	runWasmtimeAdaptedEquivalent(t, "big-memory-behavior", 1, 6)
}

func TestWasmtimeMemoryCombosCore3Projection(t *testing.T) {
	runWasmtimeAdaptedEquivalent(t, "memory-combos", 1, 64)
}

func TestWasmtimeMemoryFillCore3Projection(t *testing.T) {
	runWasmtimeAdaptedEquivalent(t, "memory_fill", 1, 26)
}

func TestWasmtimeMemory64Near4GiBBoundedEquivalent(t *testing.T) {
	runWasmtimeAdaptedEquivalent(t, "memory64/more-than-4gb", 8, 7)
}

func TestWasmtimeTable64TooBigCoreEquivalent(t *testing.T) {
	runWasmtimeAdaptedEquivalent(t, "memory64/table-too-big", 1, 2)
}

func runWasmtimeAdaptedEquivalent(t *testing.T, fixture string, wantModules, wantAssertions int) {
	t.Helper()
	dir := filepath.Join(filepath.Clean("../../tests/regressions/wasmtime-core3/adapted"), fixture, "equivalent")
	modules, assertions, err := wasmtimecore3.ValidateFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	if modules != wantModules || assertions != wantAssertions {
		t.Fatalf("equivalent accounting = %d modules, %d assertions; want %d, %d", modules, assertions, wantModules, wantAssertions)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sf specExecFile
	if err := regressiontest.DecodeStrictJSON(raw, &sf); err != nil {
		t.Fatal(err)
	}
	stats := runSpecExecFileWithConfig(t, fixture+"/equivalent", dir, sf, wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3))
	if stats.modulesPassed != modules || stats.assertionsPassed != assertions || stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
		t.Fatalf("equivalent stats = %+v, want %d modules and %d assertions with no gaps", stats, modules, assertions)
	}
}

func runWasmtimeCore3FixtureInProcess(t *testing.T, fixture string) specExecStats {
	t.Helper()
	dir := filepath.Join(filepath.Clean("../../tests/regressions/wasmtime-core3/core"), strings.TrimSuffix(fixture, ".wast"))
	if _, _, err := wasmtimecore3.ValidateFixture(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sf specExecFile
	if err := regressiontest.DecodeStrictJSON(raw, &sf); err != nil {
		t.Fatal(err)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)
	gcImport := wago.HostFunc(func(module wago.HostModule, _, _ []uint64) {
		collector, ok := module.(wago.GCHostModule)
		if !ok {
			panic("wasmtime.gc called without a collector-backed host module")
		}
		if err := collector.CollectGC(); err != nil {
			panic(err)
		}
	})
	return runSpecExecFileWithConfigAndImports(t, fixture, dir, sf, cfg, wago.Imports{"wasmtime.gc": gcImport})
}

func runWasmtimeCore3FixtureChild(t *testing.T, fixture string) specExecStats {
	t.Helper()
	timeout := regressiontest.Timeout(t, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	nonce := regressiontest.NewNonce(t)
	args := []string{"-test.run=^TestWasmtimeCore3Corpus$", "-test.count=1"}
	args = append(args, regressiontest.CoverageArgs()...)
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	regressiontest.PrepareCommand(cmd)
	cmd.Env = regressiontest.ChildEnvironment(map[string]string{
		wasmtimeCore3FixtureEnv:    fixture,
		regressiontest.ProtocolEnv: regressiontest.Protocol,
		regressiontest.NonceEnv:    nonce,
		"WAGO_BOUNDS":              regressiontest.ExpectedBounds,
	})
	capture := regressiontest.NewCapture(8<<10, regressionFixtureOutcomeMarker)
	cmd.Stdout, cmd.Stderr = capture, capture
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("fixture exceeded %s deadline\n%s", timeout, capture.Output())
	}
	markers := capture.Markers()
	if err != nil {
		t.Fatalf("fixture child failed: %v\n%s", err, capture.Output())
	}
	if len(markers) != 1 {
		t.Fatalf("fixture child emitted %d outcomes, want one\n%s", len(markers), capture.Output())
	}
	var outcome regressionFixtureOutcome
	if err := regressiontest.DecodeStrictJSON([]byte(markers[0]), &outcome); err != nil {
		t.Fatalf("decode child outcome: %v\n%s", err, capture.Output())
	}
	if outcome.Protocol != regressiontest.Protocol || outcome.Fixture != fixture || outcome.Nonce != nonce {
		t.Fatalf("child identity = %q %q %q, want %q %q %q", outcome.Protocol, outcome.Fixture, outcome.Nonce, regressiontest.Protocol, fixture, nonce)
	}
	return outcome.stats()
}
