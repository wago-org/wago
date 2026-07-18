//go:build linux && riscv64

package wago

import (
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRISCV64RVVTierHookUsesRuntimeDetector(t *testing.T) {
	if got, want := detectRISCV64SIMDHostFeatures(), coreruntime.RISCV64HasRVV(); got != want {
		t.Fatalf("RVV tier hook = %v, runtime detector = %v", got, want)
	}
	t.Logf("rvv=%v", coreruntime.RISCV64HasRVV())
}
