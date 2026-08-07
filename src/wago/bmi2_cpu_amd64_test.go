//go:build amd64

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestBMI2OptimizationHostGateAndCodecRequirement(t *testing.T) {
	original := bmi2HostFeaturesSupported
	defer func() { bmi2HostFeaturesSupported = original }()

	bmi2HostFeaturesSupported = func() bool { return false }
	unsupported := NewRuntimeConfig()
	for _, info := range unsupported.OptimizationInfos() {
		if info.Name == "bmi2-rorx" && info.On {
			t.Fatal("BMI2 optimization defaulted on for an unsupported host")
		}
	}
	if err := unsupported.WithOptimization("bmi2-rorx", true).Validate(); err == nil || !strings.Contains(err.Error(), "requires BMI2") {
		t.Fatalf("unsupported BMI2 selection error = %v", err)
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
	compiled, err := cfg.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !compiled.RequiresBMI2() {
		t.Fatal("compiled RORX module did not record BMI2")
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	bmi2HostFeaturesSupported = func() bool { return false }
	var rejected Compiled
	if err := rejected.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "requires BMI2") {
		t.Fatalf("unsupported-host unmarshal error = %v", err)
	}

	bmi2HostFeaturesSupported = func() bool { return true }
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !loaded.RequiresBMI2() {
		t.Fatal("round-tripped module lost BMI2 requirement")
	}
}
