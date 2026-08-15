//go:build arm64

package arm64

import (
	"os"
	"testing"
)

// Backend feature tests exercise the opt-in paths directly. Public defaults are
// verified by the optimization catalog and runtime configuration tests.
func TestMain(m *testing.M) {
	callEffectBoundsEnabled = true
	mergeNextUseEnabled = true
	mergeRegResidencyEnabled = true
	abiClassesEnabled = true
	abiLeafFPEnabled = true
	preparedFPEntryEnabled = true
	deadGCNewEnabled = true
	fixedGCArrayLenEnabled = true
	constGCStructGetEnabled = true
	gcConstructorCastEnabled = true
	nativeGCFinalCastEnabled = true
	nativeGCFinalArrayLenEnabled = true
	nativeGCFinalScalarGetEnabled = true
	nativeGCFinalScalarSetEnabled = true
	nativeGCFinalRefGetEnabled = true
	nativeGCFinalRefSetEnabled = true
	nativeGCFinalArrayScalarGetEnabled = true
	nativeGCFinalArrayScalarSetEnabled = true
	nativeGCResolveReuseEnabled = true
	callRematConstEnabled = true
	callRematLocalEnabled = true
	callRematBinEnabled = true
	inlineSlotOverlayEnabled = true
	entryInitElisionEnabled = true
	callResultResidencyEnabled = true
	indirectCallResultResidencyEnabled = true
	immutableLocalPolyFastPath = true
	loopPrecheckEnabled = true
	os.Exit(m.Run())
}
