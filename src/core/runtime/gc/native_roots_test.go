package gc

import "testing"

func TestCollectorNativeRootMapContract(t *testing.T) {
	maps := []NativeRootMap{{
		LocalFunction: 0,
		FrameBytes:    336,
		Slots:         []NativeRootSlot{{Offset: 248, Kind: NativeRootFuncRef}},
	}}
	if err := ValidateNativeRootMaps(maps, 1); err != nil {
		t.Fatalf("collector native root map: %v", err)
	}
	if maps[0].Slots[0].Kind == NativeRootGCRef {
		t.Fatal("funcref lifecycle root was misclassified as a collector gc.Ref")
	}
}
