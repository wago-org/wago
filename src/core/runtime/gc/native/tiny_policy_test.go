package gc

import "testing"

func requireTinyIncrementalBuild(t testing.TB) {
	t.Helper()
	if !tinyIncrementalBuild {
		t.Skip("requires the default incremental Tiny build")
	}
}
