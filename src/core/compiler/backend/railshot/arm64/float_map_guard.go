//go:build arm64

package arm64

import "os"

var floatMapGuardSelectionEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_GUARD_SELECTION") != "1"

// Keep the address domain explicit: dynamic Wasm i32 additions wrap before
// widened memory immediates and range endpoints are added. The zero W-form
// still canonicalizes the result's upper bits.
func (f *fn) floatMapOffset(dst, base Reg, offset uint64, wide bool, scratch Reg) {
	if floatMapGuardSelectionEnabled {
		if offset == 0 {
			if !wide {
				f.a.MovReg32(dst, base)
			} else if dst != base {
				f.a.MovReg64(dst, base)
			}
			return
		}
		if offset <= 4095 {
			if wide {
				f.a.AddImm64(dst, base, uint32(offset))
			} else {
				f.a.AddImm32(dst, base, uint32(offset))
			}
			return
		}
	}
	f.a.MovImm64(scratch, offset)
	if wide {
		f.a.Add64(dst, base, scratch)
	} else {
		f.a.Add32(dst, base, scratch)
	}
}
