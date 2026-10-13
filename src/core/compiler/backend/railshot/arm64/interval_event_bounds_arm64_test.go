//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestIntervalEventMaximumBodyCoordinates(t *testing.T) {
	prefix := maxIntervalRegionBody - 3
	body := make([]byte, prefix)
	for i := range body {
		body[i] = 0x01
	}
	body = append(body, 0x21, 0, 0x0b)
	f := fn{m: &wasm.Module{}, nLocals: 1, localType: []machineType{mtI32}, intervalLast: []uint32{uint32(prefix)}, intervalScore: []uint32{8}, intervalNext: true, tracePCBase: 123, wasmPC: 123}
	f.classifier = wasm.NewModuleInstructionClassifier(f.m, true)
	f.prepareIntervalEvents(body, 1, false)
	if next, dead := f.nextIntervalLocalAccess(0); next != uint32(prefix) || !dead {
		t.Fatalf("last overwrite coordinate=(%d,%v), want (%d,true)", next, dead, prefix)
	}
	f.wasmPC = f.tracePCBase + uint32(prefix+2)
	if next, dead := f.nextIntervalLocalAccess(0); next != noIntervalEvent || !dead {
		t.Fatalf("terminal link=(%d,%v)", next, dead)
	}
	f.prepareIntervalEvents(append(body, 0x01), 1, false)
	if f.nextUsePolicy() {
		t.Fatal("oversized body retained a packed event proof")
	}
}
