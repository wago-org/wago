package wago

import (
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestReviewSharedProviderUnsharedImport(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	memImport := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memImport = append(memImport, 2, 3, 1, 1)
	producerBytes := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(memImport)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mem", 2, 0))),
	)
	pc, err := Compile(cfg, producerBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	hostMemory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer hostMemory.Close()
	p, err := Instantiate(pc, NewImports().Memory("env", "mem", hostMemory))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	memory, err := p.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	cc, err := Compile(cfg, importMemModule())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	consumer, err := Instantiate(cc, NewImports().Memory("env", "mem", memory))
	if consumer != nil {
		defer consumer.Close()
	}
	if err == nil {
		t.Fatal("shared Wasm memory was accepted for an unshared import")
	}
}
