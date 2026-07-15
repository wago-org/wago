//go:build arm64 && !tinygo

package wago

import (
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestStagedMemory64FailsClosedOnArm64(t *testing.T) {
	module := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x05, 0x01, 0x02})))
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Memory64 = true
	_, err := compileWithFrontendFeatures(cfg, module, features)
	want := "unsupported memory memory64 staged execution on " + runtime.GOOS + "/arm64"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("staged memory64 arm64 error = %v, want %q", err, want)
	}
}
