//go:build arm64

package arm64

import (
	"os"
)

var intervalCallRegionsEnabled = os.Getenv("WAGO_ARM64_NO_INTERVAL_CALL_REGIONS") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS") != "0"

var intervalControlsEnabled = os.Getenv("WAGO_ARM64_INTERVAL_CONTROLS") != "0"

const (
	noIntervalEvent   = ^uint32(0)
	intervalEventKill = uint32(1 << 15)
)

// Region bodies are bounded at 16 KiB. Offsets fit below bit 15, and
// there is at most one event per opcode, so event links fit in uint16 too.
type intervalLocalEvent struct {
	pos  uint16
	next uint16
}

const (
	minIntervalRegionBody        = 128
	minIntervalRegionLocals      = 32
	maxIntervalRegionBody        = 16 << 10
	maxIntervalRegionLocals      = 256
	maxIntervalRegionRegs        = 19
	intervalRegionTransientFloor = 3
)

func intervalRegionRegLimit(reserved regMask) int {
	return intervalRegionRegLimitWithFloor(reserved, intervalRegionTransientFloor)
}

var intervalRegionOrder = [...]Reg{
	X19, X20, X21, X22, X23, X24, X25, X27,
	X9, X10, X11, X12, X13, X14, X15, X8,
	X7, X6, X5, X4, X3, X2,
}

func (f *fn) intervalLocalHasMemBorrow(x int) bool {
	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		if e.elemKind() == ekValue && e.st.kind == stMemRef && e.st.memBorrow() == x {
			return true
		}
	}
	return false
}

func saturatingWideTraffic(current, addition uint32) uint32 {
	if current > ^uint32(0)-addition {
		return ^uint32(0)
	}
	return current + addition
}
