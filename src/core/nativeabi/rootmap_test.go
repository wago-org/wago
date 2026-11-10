package nativeabi

import (
	"fmt"
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

func TestValidateRootMapsFunctionBounds(t *testing.T) {
	for _, index := range []uint32{0, 1, 2, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff} {
		t.Run(fmt.Sprintf("%08x", index), func(t *testing.T) {
			err := ValidateRootMaps([]FunctionRootMap{{LocalFunction: index, FrameBytes: 8}}, 2)
			if index < 2 {
				if err != nil {
					t.Fatalf("valid function index: %v", err)
				}
			} else if want := fmt.Sprintf("function %d out of range", index); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("root map error = %v, want %q", err, want)
			}
		})
	}
}

func TestValidateRootMapsRejectsNegativeFunctionCount(t *testing.T) {
	for _, count := range []int{-1, -int(^uint(0)>>1) - 1} {
		if err := ValidateRootMaps(nil, count); err == nil || err.Error() != "negative local function count" {
			t.Errorf("count %d error = %v, want negative local function count", count, err)
		}
	}
	if err := ValidateRootMaps(nil, 0); err != nil {
		t.Fatalf("empty root maps: %v", err)
	}
}

func BenchmarkValidateRootMapsFunctionBounds(b *testing.B) {
	maps := []FunctionRootMap{{LocalFunction: 0, FrameBytes: 8, Slots: []RootSlot{{Offset: 0, Kind: RootGCRef}}}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := ValidateRootMaps(maps, 1); err != nil {
			b.Fatal(err)
		}
	}
}
