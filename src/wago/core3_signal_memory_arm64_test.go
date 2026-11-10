//go:build (linux || darwin) && arm64 && wago_guardpage && !tinygo

package wago

import (
	"reflect"
	"testing"
)

func TestARM64SignalBackedMultiMemory(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMultiMemory).WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, arm64MultiMemoryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("store1", 65532, 0x12345678); err != nil {
		t.Fatal(err)
	}
	if got, err := in.Invoke("load1", 65532); err != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
		t.Fatalf("load1 = %v, %v", got, err)
	}
	if _, err := in.Invoke("load1", 65536); err == nil {
		t.Fatal("indexed memory signal product accepted out-of-bounds load")
	}
	if _, err := in.Invoke("copy10_const"); err != nil {
		t.Fatal(err)
	}
}

func TestARM64SignalBackedMemory64(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMemory64).WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, arm64Memory64Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("store_load", 65532, 0x12345678); err != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
		t.Fatalf("store_load = %v, %v", got, err)
	}
	if _, err := in.Invoke("store_load", uint64(1)<<32, 1); err == nil {
		t.Fatal("signal-backed memory64 accepted high address")
	}
	if _, err := in.Invoke("offset_load", 1); err == nil {
		t.Fatal("signal-backed memory64 accepted wrapping static offset")
	}
}
