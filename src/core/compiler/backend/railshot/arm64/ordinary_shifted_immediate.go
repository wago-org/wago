//go:build arm64

package arm64

import "os"

var ordinaryShiftedImmediateEnabled = os.Getenv("WAGO_ARM64_NO_ORDINARY_SHIFTED_IMMEDIATE") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE") != "0"

var shiftedALUComparePaddingEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_ALU_COMPARE_PADDING") != "1"

var shiftedALUImmediateEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_ALU_IMMEDIATE") != "1"

// Prefer scarce immutable leases for constants without an immediate form.
var shiftedImmediateHintFilterEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_IMMEDIATE_HINT_FILTER") != "1"

func discountShiftedImmediateHint(consumer byte, value int64) bool {
	if !shiftedImmediateHintFilterEnabled || !shiftedAddSubImmediateEnabled || !ordinaryShiftedImmediateEnabled || !fitsShiftedAddSubImmediate(value) {
		return false
	}
	return consumer == 0x6a || consumer == 0x6b || consumer == 0x7c || consumer == 0x7d
}
