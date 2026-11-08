//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var integerLoadMapEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_LOAD_MAP") != "1"

const integerLoadMapWorkLimit = 4096

// Integer recipes use the same owned candidate, source-order Q groups, widened
// memory immediates, nontrapping eligibility guards and scalar tail as floats.
func (p *floatMapPlan) valueType() machineType {
	if p.integer {
		return mtI32
	}
	return mapFloatType(p.wide)
}
func (p *floatMapPlan) load(op byte) bool {
	if p.integer {
		return op == 0x28
	}
	return op == 0x2a || op == 0x2b
}
func (p *floatMapPlan) constant(op byte) bool {
	if p.integer {
		return op == 0x41
	}
	return op == 0x43 || op == 0x44
}
func (p *floatMapPlan) binary(op byte) bool {
	if p.integer {
		switch op {
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73:
			return true
		}
		return false
	}
	return op >= 0x92 && op <= 0x95 || op >= 0xa0 && op <= 0xa3
}
func (p *floatMapPlan) spend() bool {
	if !p.integer {
		return true
	}
	p.work++
	return p.work <= p.workLimit
}

func (f *fn) emitIntegerLoadMap(source *wasm.Reader) {
	if !integerLoadMapEnabled || !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	if len(f.intervalLast) != 0 && (!integerMapIntervalsEnabled || !f.intervalControl || f.intervalActive != 0) {
		return
	}
	// Bound failed screens and all admitted planning walks across this function.
	// The large candidate frame is reached only after finding a typed load.
	scan := *source
	for ops := 0; ops < 24 && f.integerLoadMapWork < integerLoadMapWorkLimit; ops++ {
		f.integerLoadMapWork++
		op, e := scan.Byte()
		if e != nil {
			return
		}
		if op == 0x28 {
			f.emitIntegerLoadMapCandidate(source)
			return
		}
		switch op {
		case 0x20, 0x21, 0x22:
			if _, e := scan.U32(); e != nil {
				return
			}
		case 0x41:
			if _, e := scan.I32(); e != nil {
				return
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73:
			// These fixed-width operators have no immediates. Keeping the
			// tiny screen concrete avoids making its copied reader escape
			// through the generic classifier on every rejected loop.
		default:
			return
		}
	}
}

// Keep the scoped reset outside the screen's loop. A defer declared inside a
// Go loop heap-allocates its closure even when that branch immediately returns.
func (f *fn) emitIntegerLoadMapCandidate(source *wasm.Reader) {
	previous := f.integerLoadMapProbe
	f.integerLoadMapProbe = true
	defer func() { f.integerLoadMapProbe = previous }()
	f.emitFloatMapBody(source)
}
