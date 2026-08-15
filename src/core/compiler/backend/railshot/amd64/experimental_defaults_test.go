//go:build amd64

package amd64

import (
	"os"
	"testing"
)

// Backend feature tests exercise the opt-in paths directly. Public defaults are
// verified by the optimization catalog and runtime configuration tests.
func TestMain(m *testing.M) {
	callEffectBoundsEnabled = true
	deadGCNewEnabled = true
	preparedFPEntryEnabled = true
	abiClassesEnabled = true
	abiLeafFPEnabled = true
	mergeNextUseEnabled = true
	entryInitElisionEnabled = true
	callRematConstEnabled = true
	callRematLocalEnabled = true
	callRematBinEnabled = true
	callResultResidencyEnabled = true
	mergeRegResidencyEnabled = true
	inlineSlotOverlayEnabled = true
	loopPrecheckEnabled = true
	os.Exit(m.Run())
}
