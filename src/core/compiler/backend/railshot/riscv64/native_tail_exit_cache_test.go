//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestTailInvocationExitPublicationCleanupRetry(t *testing.T) {
	var state nativeTailExitCache
	publishFailure, cleanupFailure := errors.New("exit publication failed"), errors.New("exit cleanup failed")
	allocations, publications, cleanups := 0, 0, 0
	build := func() ([]byte, error) { return []byte{0x13, 0, 0, 0}, nil }
	allocate := func(n int) (*coreruntime.CodeBuffer, error) { allocations++; return coreruntime.NewCodeBuffer(n) }
	publish := func(b *coreruntime.CodeBuffer) error {
		publications++
		if publications == 1 {
			return publishFailure
		}
		return b.Seal()
	}
	cleanup := func(b *coreruntime.CodeBuffer) error {
		cleanups++
		if cleanups <= 2 {
			return cleanupFailure
		}
		return b.Close()
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
	if err := state.code.Close(); err != nil {
		t.Fatal(err)
	}
}
