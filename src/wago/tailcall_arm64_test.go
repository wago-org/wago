//go:build arm64

package wago

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestArm64TailCallFeatureRemainsFailClosed(t *testing.T) {
	if SupportedFeatures().IsEnabled(CoreFeatureTailCall) {
		t.Error("arm64 must not advertise tail-call execution before backend parity")
		return
	}
	err := NewRuntimeConfig().WithFeature(CoreFeatureTailCall, true).Validate()
	var unsupported *UnsupportedFeatureError
	if !errors.As(err, &unsupported) {
		t.Errorf("Validate error = %v, want UnsupportedFeatureError", err)
		return
	}
	if unsupported.Requested != CoreFeatureTailCall || unsupported.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("unsupported tail-call metadata = %+v", unsupported)
	}
}

func TestStagedDirectTailCallsFailClosedOnArm64(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x12, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x0b}),
		)),
	)
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TailCalls = true
	_, err := compileWithFrontendFeatures(cfg, module, features)
	want := "unsupported instruction tail-call staged execution on " + runtime.GOOS + "/arm64"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("staged arm64 direct-tail error = %v, want %q", err, want)
	}
}
