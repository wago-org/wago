//go:build amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBMI2OptimizationHostGateAndCodecRequirement(t *testing.T) {
	mockAMD64ArtifactCPU(t, shared.AMD64BMI2)
	original := bmi2HostFeaturesSupported
	defer func() { bmi2HostFeaturesSupported = original }()

	bmi2HostFeaturesSupported = func() bool { return false }
	unsupported := NewRuntimeConfig()
	for _, info := range unsupported.OptimizationInfos() {
		if info.Name == "bmi2-rorx" && info.On {
			t.Fatal("BMI2 optimization defaulted on for an unsupported host")
		}
	}
	if err := unsupported.WithOptimization("bmi2-rorx", true).Validate(); err != nil {
		t.Fatalf("optional BMI2 selection must retain its fallback: %v", err)
	}

	bmi2HostFeaturesSupported = func() bool { return true }
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	found := false
	for _, info := range cfg.OptimizationInfos() {
		if info.Name == "bmi2-rorx" {
			found = true
			if !info.On {
				t.Fatal("BMI2 optimization was not selected on a supported host")
			}
		}
	}
	if !found {
		t.Fatal("BMI2 optimization is absent from the amd64 catalog")
	}

	body := []byte{0x20, 0x00, 0x41, 0x07, 0x78, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	bmi2HostFeaturesSupported = func() bool { return false }
	fallback, err := unsupported.WithOptimization("bmi2-rorx", true).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.RequiresBMI2() {
		t.Fatal("missing BMI2 did not select the rotate fallback")
	}
	fallback.Close()
	bmi2HostFeaturesSupported = func() bool { return true }
	compiled, err := cfg.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !selectedAMD64CompileFeatures(shared.AMD64BMI2).Has(shared.AMD64BMI2) {
		if compiled.RequiresBMI2() {
			t.Fatal("baseline profile emitted BMI2")
		}

		bmi2HostFeaturesSupported = func() bool { return false }

		compiled.Close()
		return
	}
	if !compiled.RequiresBMI2() {
		t.Fatal("compiled RORX module did not record BMI2")
	}

	bmi2HostFeaturesSupported = func() bool { return false }

	bmi2HostFeaturesSupported = func() bool { return true }

	if !compiled.RequiresBMI2() {
		t.Fatal("round-tripped module lost BMI2 requirement")
	}
}
