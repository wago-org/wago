//go:build linux && riscv64 && !tinygo && wago_precompiled

package wago

import "testing"

func rv64PrepareWideData(t *testing.T, _ *Runtime, _ byte, _ []byte) (*Module, []byte) {
	t.Helper()
	t.Fatal("compiler-free custom checksum tests require load mode")
	return nil, nil
}
