package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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
