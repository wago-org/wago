//go:build arm64

package wago

import (
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestStagedTable64FailsClosedOnArm64(t *testing.T) {
	module := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0x01, 0x02})))
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	_, err := compileWithFrontendFeatures(cfg, module, features)
	want := "unsupported table table64 staged execution on " + runtime.GOOS + "/arm64"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("staged table64 arm64 error = %v, want %q", err, want)
	}
}
