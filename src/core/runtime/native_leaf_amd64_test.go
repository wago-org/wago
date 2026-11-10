//go:build (linux || darwin || windows) && amd64 && !tinygo

package runtime

import "testing"

func TestScalarLeafRejectsUnsafeInstructions(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []byte
	}{
		{"memory read", []byte{0x8b, 0x00}}, {"closure read", []byte{0x48, 0x8b, 0x42, 8}},
		{"rip address", []byte{0x8d, 0x05, 0, 0, 0, 0}}, {"stack address", []byte{0x8d, 0x04, 0x24}},
		{"stack destination", []byte{0x89, 0xc4}}, {"frame destination", []byte{0x89, 0xc5}},
		{"native pinned destination", []byte{0x41, 0x89, 0xc4}}, {"uninitialized input", []byte{0x01, 0xd0}},
		{"carry input", []byte{0x11, 0xc0}}, {"rotate through carry", []byte{0xd1, 0xd0}},
		{"flags select", []byte{0x0f, 0x44, 0xc1}}, {"flags byte", []byte{0x0f, 0x94, 0xc0}},
		{"call", []byte{0xe8, 0, 0, 0, 0}}, {"indirect call", []byte{0xff, 0xd0}},
		{"indirect jump", []byte{0xff, 0xe0}}, {"syscall", []byte{0x0f, 0x05}},
		{"division", []byte{0xf7, 0xf9}}, {"fp", []byte{0x66, 0x0f, 0xef, 0xc0}},
		{"partial destination", []byte{0x66, 0x89, 0xc0}}, {"truncated immediate", []byte{0xb8, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defined := uint32(1 | 1<<3)
			if _, ok := scalarLeafInstruction(tc.code, &defined); ok {
				t.Fatalf("unsafe bytes %x admitted", tc.code)
			}
		})
	}
}

func TestScalarLeafTracksDefinedScratch(t *testing.T) {
	defined := uint32(1)
	for _, code := range [][]byte{{0x35, 0x78, 0x56, 0x34, 0x12}, {0x31, 0xd2}, {0x01, 0xd0}, {0x8d, 0x04, 0x40}, {0xc1, 0xf8, 13}, {0x48, 0x63, 0xc0}, {0x48, 0xc1, 0xf8, 33}} {
		n, ok := scalarLeafInstruction(code, &defined)
		if !ok || n != len(code) {
			t.Fatalf("safe arithmetic %x rejected", code)
		}
	}
}

func TestScalarLeafBodyRequiresI32ABI(t *testing.T) {
	if scalarLeafBody(func(v int64) int64 { return v + 1 }, 1) != nil {
		t.Fatal("i64 signature admitted as i32")
	}
	if scalarLeafBody(nativeLeafIncTest, 1) == nil {
		t.Fatal("pure i32 leaf rejected")
	}
	if scalarLeafBody(nativeLeafPairTest, 2) == nil {
		t.Fatal("pure pair leaf rejected")
	}
	captured := int32(19)
	if scalarLeafBody(func(v int32) int32 { captured++; return v + captured }, 1) != nil {
		t.Fatal("mutable captured state admitted")
	}
}

//go:norace
//go:noinline
func nativeLeafIncTest(v int32) int32 { return v + 1 }

//go:norace
//go:noinline
func nativeLeafPairTest(a, b int32) int32 { return a*b + a - b }
