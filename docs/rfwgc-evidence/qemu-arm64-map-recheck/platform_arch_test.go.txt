package wago

import (
	"os"
	"runtime"
	"testing"
)

func TestPlatformExecutionArchitecture(t *testing.T) {
	want := os.Getenv("WAGO_EXPECT_ARCH")
	if runtime.GOARCH != want || runtime.GOOS != "linux" {
		t.Fatalf("actual %s/%s expected linux/%s", runtime.GOOS, runtime.GOARCH, want)
	}
	t.Logf("EXECUTED linux/%s", runtime.GOARCH)
}
