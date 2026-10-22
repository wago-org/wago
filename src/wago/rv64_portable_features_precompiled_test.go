//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage && wago_precompiled

package wago

import "testing"

func rv64PreparePortableFeature(t *testing.T, name string) []byte {
	t.Helper()
	t.Fatal("compiler-free feature tests require load mode: ", name)
	return nil
}
