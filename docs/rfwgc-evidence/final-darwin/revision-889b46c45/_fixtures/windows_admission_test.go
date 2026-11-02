//go:build windows && amd64 && !tinygo

package wago

import (
	"errors"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// Windows is not an admitted Core 3 target. This is an admission contract,
// not execution of converted guest code or proof of Windows GC JIT support.
func TestQualificationWindowsGCAdmission(t *testing.T) {
	if SupportedFeatures().IsEnabled(CoreFeatureGC) {
		t.Fatal("unexpected Windows GC admission")
	}
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module())
	if code != nil {
		code.Close()
		t.Fatal("unexpected Core 3 compilation")
	}
	var unsupported *UnsupportedFeatureError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected feature rejection, got %v", err)
	}
}
