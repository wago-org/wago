//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

import "testing"

func TestScalarLeafRejectsUnsafeInstructions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		instruction uint32
	}{
		{"stack address", 0x910003e0}, // ADD X0,SP,#0
		{"stack destination", 0x9100001f},
		{"uninitialized source", 0x91000440},      // ADD X0,X2,#1
		{"native pinned destination", 0x91000413}, // ADD X19,X0,#1
		{"memory read", 0xf9400000},
		{"branch", 0x14000001},
		{"call", 0x94000001},
		{"closure read", 0x91000340}, // ADD X0,X26,#0
		{"flags-dependent select", 0x9a800000},
		{"reserved multiply", 0x5b007c00},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defined := uint32(3)
			if scalarLeafInstruction(tc.instruction, &defined) {
				t.Fatalf("unsafe instruction %#08x admitted", tc.instruction)
			}
		})
	}
}

func TestScalarLeafBodyRequiresI32ABI(t *testing.T) {
	if scalarLeafBody(func(v int64) int64 { return v + 1 }, 1) != nil {
		t.Fatal("i64 signature admitted as i32")
	}
	if scalarLeafBody(nativeLeafIncTest, 1) == nil {
		t.Fatal("pure i32 leaf rejected")
	}
}

//go:norace
//go:noinline
func nativeLeafIncTest(v int32) int32 { return v + 1 }

func TestScalarGoABIRejectsUnverifiedVersions(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"go1.22.0", true}, {"go1.27.1", true}, {"go1.27", true},
		{"go1.21.13", false}, {"go1.28.0", false}, {"devel go1.27", false},
		{"go1.27rc1", false}, {"tinygo", false}, {"go2.0.0", false},
	} {
		if scalarGoABI(tc.version) != tc.want {
			t.Errorf("version %s: want %t", tc.version, tc.want)
		}
	}
}
