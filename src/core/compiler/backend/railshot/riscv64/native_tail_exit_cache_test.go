//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestTailInvocationExitPublicationCleanupRetry(t *testing.T) {
	var state nativeTailExitCache
	publishFailure, cleanupFailure := errors.New("exit publication failed"), errors.New("exit cleanup failed")
	allocations, publications, cleanups := 0, 0, 0
	build := func() ([]byte, error) { return []byte{0x13, 0, 0, 0}, nil }
	allocate := func(code []byte) ([]byte, uintptr, error) {
		allocations++
		return coreruntime.MapWritableCode(code)
	}
	publish := func(b []byte) error {
		publications++
		if publications == 1 {
			return publishFailure
		}
		return coreruntime.SealCode(b)
	}
	cleanup := func(b []byte) error {
		cleanups++
		if cleanups <= 2 {
			return cleanupFailure
		}
		return coreruntime.Unmap(b)
	}
	pointer, err := state.get(build, allocate, publish, cleanup)
	if pointer != 0 || !errors.Is(err, publishFailure) || !errors.Is(err, cleanupFailure) || state.code == nil {
		t.Fatal("failed exit owner lost", err)
	}
	pointer, err = state.get(build, allocate, publish, cleanup)
	if pointer != 0 || !errors.Is(err, cleanupFailure) || allocations != 1 || publications != 1 {
		t.Fatal("published before retained failure cleaned", err)
	}
	pointer, err = state.get(build, allocate, publish, cleanup)
	if err != nil || pointer == 0 || allocations != 2 || publications != 2 || cleanups != 3 {
		t.Fatal("publication retry", err)
	}
	stable, err := state.get(build, allocate, publish, cleanup)
	if err != nil || stable != pointer || allocations != 2 || publications != 2 || cleanups != 3 {
		t.Fatal("successful identity changed", err)
	}
	if err := coreruntime.Unmap(state.code); err != nil {
		t.Fatal(err)
	}
}
