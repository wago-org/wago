//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func rv64PublicGCTableRoots(t testing.TB, in *Instance, count int) []gc.RootSlot {
	t.Helper()
	var roots []gc.RootSlot
	in.rangeLocalGCTableRoots(func(slot gc.RootSlot) bool { roots = append(roots, slot); return true }, nil)
	if len(roots) != count {
		t.Fatalf("table roots=%d, want %d", len(roots), count)
	}
	return roots
}
