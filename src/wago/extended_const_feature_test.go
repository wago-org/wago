package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func extendedConstFeatureModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{
			0x41, 0x01, // i32.const 1
			0x41, 0x02, // i32.const 2
			0x6a, // i32.add
			0x0b,
		}))),
	)
}

func extendedConstImportedGlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "base", wasm.I32, false))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{
			0x23, 0x00, // global.get 0
			0x41, 0x02, // i32.const 2
			0x6a, // i32.add
			0x0b,
		}))),
	)
}

func TestExtendedConstRespectsCoreFeatureConfiguration(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1)
	if _, err := Compile(cfg, extendedConstFeatureModule()); err == nil || !strings.Contains(err.Error(), "extended-constant-expressions disabled") {
		t.Fatalf("MVP compile error = %v, want extended-constant feature rejection", err)
	}
	if _, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureExtendedConst, false), extendedConstFeatureModule()); err == nil || !strings.Contains(err.Error(), "extended-constant-expressions disabled") {
		t.Fatalf("explicitly disabled compile error = %v, want extended-constant feature rejection", err)
	}
	compiled, err := Compile(nil, extendedConstFeatureModule())
	if err != nil {
		t.Fatalf("default compile: %v", err)
	}
	defer compiled.Close()
	if got := CoreFeatures(compiled.requiredFeatures); got != CoreFeatureExtendedConst {
		t.Fatalf("required features = %s, want %s", got, CoreFeatureExtendedConst)
	}
}

func TestExtendedConstRequiredFeatureSurvivesCodecAndFailsClosed(t *testing.T) {
	compiled := compileExplicitArtifact(t, extendedConstImportedGlobalModule())
	defer compiled.Close()
	loaded := roundTripCompiled(t, compiled)
	if got := CoreFeatures(loaded.requiredFeatures); got != CoreFeatureExtendedConst {
		t.Fatalf("loaded required features = %s, want %s", got, CoreFeatureExtendedConst)
	}

	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got := CoreFeatures(blob[len(blob)-2]); got != CoreFeatureExtendedConst || blob[len(blob)-1] != 0 {
		t.Fatalf("codec feature tail = %x, want extended-constant then zero GC count", blob[len(blob)-2:])
	}
	blob[len(blob)-2] = 0
	var missing Compiled
	if err := missing.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unrecorded features") {
		t.Fatalf("missing extended-constant bit error = %v", err)
	}
}
