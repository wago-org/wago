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
		if (info.Name == "bmi2-rorx" || info.Name == "bmi2-shifts") && info.On {
			t.Fatal("BMI2 optimization defaulted on for an unsupported host")
		}
	}
	for _, name := range []string{"bmi2-rorx", "bmi2-shifts"} {
		if err := unsupported.WithOptimization(name, true).Validate(); err == nil || !strings.Contains(err.Error(), "requires BMI2") {
			t.Fatalf("unsupported %s selection error = %v", name, err)
		}
	}

	bmi2HostFeaturesSupported = func() bool { return true }
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	found := map[string]bool{"bmi2-rorx": false, "bmi2-shifts": false}
	for _, info := range cfg.OptimizationInfos() {
		if _, ok := found[info.Name]; ok {
			found[info.Name] = true
			if !info.On {
				t.Fatalf("%s was not selected on a supported host", info.Name)
			}
		}
	}
	for name, ok := range found {
		if !ok {
			t.Fatalf("%s is absent from the amd64 catalog", name)
		}
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
