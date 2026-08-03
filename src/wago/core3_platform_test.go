package wago

import (
	goruntime "runtime"
	"testing"
)

func requireCompleteCore3Backend(t testing.TB) {
	t.Helper()
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
		t.Skipf("complete Core 3 execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
}
