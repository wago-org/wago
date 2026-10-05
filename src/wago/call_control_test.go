//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRefreshNativeControlPreservesQueuedInterruption(t *testing.T) {
	jm, err := coreruntime.NewJobMemoryGrowable(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	eng, err := coreruntime.AcquireEngineWithStackBytes(coreruntime.DefaultNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.ReleaseEngine(eng)
	old, active := make([]byte, coreruntime.TrapBufferBytes), make([]byte, coreruntime.TrapBufferBytes)
	if err := jm.BindTrapCell(old); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(active, uint32(coreruntime.TrapInterrupted))
	if err := refreshNativeControl(true, eng, jm, active); err != nil {
		t.Fatal(err)
	}
	if !jm.HasTrapCell(active) || binary.LittleEndian.Uint32(active) != uint32(coreruntime.TrapInterrupted) {
		t.Fatal("rebinding erased interruption or kept a foreign trap pointer")
	}
}
