package wago

import (
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestNativeOpaqueWordRejectsTypedMarker(t *testing.T) {
	state := gcExternConversionState{store: &referenceStore{}}
	for range 64 {
		word, err := state.newOpaqueWordLocked()
		if err != nil {
			t.Fatal(err)
		}
		if word>>32 == 0 || word&gc.NativeTypedRefKnown != 0 {
			t.Fatalf("opaque word overlaps compact or typed reference class: %#x", word)
		}
	}
}
