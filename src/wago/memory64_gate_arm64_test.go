//go:build arm64 && !tinygo

package wago

import (
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestStagedMemory64FailsClosedOnArm64(t *testing.T) {
	imported := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imported = append(imported, 0x02, 0x05, 0x01, 0x02)
	for name, module := range map[string][]byte{
		"local":  wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x05, 0x01, 0x02}))),
		"import": wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imported))),
	} {
		t.Run(name, func(t *testing.T) {
			cfg := NewRuntimeConfig()
			features := cfg.frontendFeatures()
			features.Memory64 = true
			_, err := compileWithFrontendFeatures(cfg, module, features)
			want := "unsupported memory memory64 staged execution on " + runtime.GOOS + "/arm64"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("staged memory64 arm64 error = %v, want %q", err, want)
			}
		})
	}
}
