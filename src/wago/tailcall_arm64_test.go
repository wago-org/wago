//go:build arm64

package wago

import (
	"errors"
	"runtime"
	"testing"
)

func TestArm64TailCallFeatureRemainsFailClosed(t *testing.T) {
	if SupportedFeatures().IsEnabled(CoreFeatureTailCall) {
		t.Fatal("arm64 must not advertise tail-call execution before backend parity")
	}
	err := NewRuntimeConfig().WithFeature(CoreFeatureTailCall, true).Validate()
	var unsupported *UnsupportedFeatureError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Validate error = %v, want UnsupportedFeatureError", err)
	}
	if unsupported.Requested != CoreFeatureTailCall || unsupported.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("unsupported tail-call metadata = %+v", unsupported)
	}
}
