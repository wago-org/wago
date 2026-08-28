package wago

import (
	"crypto/sha256"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func railshotProfileModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		)),
	)
}

func TestRailshotProfileCountsNativeFunctionEntries(t *testing.T) {
	module := railshotProfileModule()
	c, err := Compile(NewRuntimeConfig().WithRailshotProfiling(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for range 3 {
		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := in.SnapshotRailshotProfile(CompilerProfileSteady, true)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ModuleHash != sha256.Sum256(module) || profile.Generation != 1 {
		t.Fatalf("profile identity = %x generation %d", profile.ModuleHash, profile.Generation)
	}
	if got, want := profile.FunctionCounts, []uint64{3, 3}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("function counts = %v, want %v", got, want)
	}
	drained, err := in.SnapshotRailshotProfile(CompilerProfileSteady, false)
	if err != nil {
		t.Fatal(err)
	}
	if drained.Generation != 2 || drained.FunctionCounts[0] != 0 || drained.FunctionCounts[1] != 0 {
		t.Fatalf("drained profile = generation %d counts %v", drained.Generation, drained.FunctionCounts)
	}
}

func TestRailshotProfileSurvivesArtifactRoundTrip(t *testing.T) {
	module := railshotProfileModule()
	c, err := Compile(NewRuntimeConfig().WithRailshotProfiling(true), module)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := c.MarshalBinary()
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := Instantiate(loaded, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	profile, err := in.SnapshotRailshotProfile(CompilerProfileStartup, false)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ModuleHash != sha256.Sum256(module) || profile.FunctionCounts[0] != 1 || profile.FunctionCounts[1] != 1 {
		t.Fatalf("round-trip profile = hash %x counts %v", profile.ModuleHash, profile.FunctionCounts)
	}
}

func TestRailshotProfilingRejectsDragline(t *testing.T) {
	cfg := NewRuntimeConfig().WithCompiler(CompilerDragline).WithRailshotProfiling(true)
	if err := cfg.Validate(); err == nil {
		t.Fatal("Dragline profiling configuration unexpectedly validated")
	}
}
