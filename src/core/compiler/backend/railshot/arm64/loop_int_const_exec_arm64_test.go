//go:build (linux || darwin) && arm64

package arm64

import "testing"

func TestLoopIntConstSwitchEquivalentArm64(t *testing.T) {
	m := loopIntConstModuleArm64(t)
	saved := loopIntConstEnabled
	defer func() { loopIntConstEnabled = saved }()
	for _, iterations := range []uintptr{1, 2, 7, 31} {
		loopIntConstEnabled = true
		on := uint32(runArm64Internal2(t, m, 3, iterations))
		loopIntConstEnabled = false
		off := uint32(runArm64Internal2(t, m, 3, iterations))
		if on != off {
			t.Fatalf("iterations=%d: enabled=%#x disabled=%#x", iterations, on, off)
		}
	}
}
