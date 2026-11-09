//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var floatCopyMapEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_COPY_MAP") != "1"

// Pure floating copies reuse the existing typed map's guarded ranges, alias
// checks and scalar continuation. Only inactive control regions may admit a
// prefix; no source-offset last-use hint is interpreted as schedule liveness.
func (f *fn) emitFloatCopyMapLoop(source *wasm.Reader) {
	if !floatCopyMapEnabled || !f.opt(optDotLoopVector) || f.floatCopyMapProbe || f.integerLoadMapProbe || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	if len(f.intervalLast) != 0 && (!integerMapIntervalsEnabled || !f.intervalControl || f.intervalActive != 0) {
		return
	}
	if !f.hasFloatCopyMapPrefix(source) {
		return
	}
	f.emitFloatCopyMapCandidate(source)
}

// The cheap screen and every admitted walk share the finite integer-map work
// budget. Failed probes leave the real bytecode reader and native state alone.
func (f *fn) hasFloatCopyMapPrefix(source *wasm.Reader) bool {
	scan := *source
	for n := 0; n < 24 && f.integerLoadMapWork < integerLoadMapWorkLimit; n++ {
		f.integerLoadMapWork++
		op, e := scan.Byte()
		if e != nil {
			return false
		}
		switch op {
		case 0x2a, 0x2b:
			return true
		case 0x20, 0x21, 0x22:
			if _, e := scan.U32(); e != nil {
				return false
			}
		case 0x41:
			if _, e := scan.I32(); e != nil {
				return false
			}
		case 0x6a, 0x6b:
		default:
			return false
		}
	}
	return false
}

func (f *fn) emitFloatCopyMapCandidate(source *wasm.Reader) {
	previous := f.floatCopyMapProbe
	f.floatCopyMapProbe = true
	defer func() { f.floatCopyMapProbe = previous }()
	f.emitFloatMapBody(source)
}
