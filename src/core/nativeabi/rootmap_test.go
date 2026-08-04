package nativeabi

import (
	"strings"
	"testing"
)

func TestValidateRootMaps(t *testing.T) {
	valid := []FunctionRootMap{
		{LocalFunction: 0, FrameBytes: 336, Slots: []RootSlot{{Offset: 248, Kind: RootFuncRef}, {Offset: 272, Kind: RootGCRef}}},
		{LocalFunction: 2, FrameBytes: 352, Slots: []RootSlot{{Offset: 264, Kind: RootGCRef}}},
	}
	if err := ValidateRootMaps(valid, 3); err != nil {
		t.Fatalf("valid root maps: %v", err)
	}
	cases := []struct {
		name string
		maps []FunctionRootMap
		want string
	}{
		{"function", []FunctionRootMap{{LocalFunction: 3, FrameBytes: 8}}, "out of range"},
		{"map order", []FunctionRootMap{{LocalFunction: 1}, {LocalFunction: 1}}, "not strictly ordered"},
		{"kind", []FunctionRootMap{{FrameBytes: 16, Slots: []RootSlot{{Offset: 0, Kind: 99}}}}, "invalid kind"},
		{"alignment", []FunctionRootMap{{FrameBytes: 16, Slots: []RootSlot{{Offset: 1, Kind: RootGCRef}}}}, "not 8-byte aligned"},
		{"frame", []FunctionRootMap{{FrameBytes: 8, Slots: []RootSlot{{Offset: 8, Kind: RootGCRef}}}}, "exceeds frame"},
		{"slot order", []FunctionRootMap{{FrameBytes: 24, Slots: []RootSlot{{Offset: 8, Kind: RootGCRef}, {Offset: 8, Kind: RootFuncRef}}}}, "not strictly ordered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRootMaps(tc.maps, 3); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateRootMaps = %v, want %q", err, tc.want)
			}
		})
	}
}
