//go:build amd64 && wago_experimental_scalar_loop_entry

package amd64

import (
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"os"
)

// -1 retains existing alignment. Values select a function-relative header
// offset in a32-byte block, not an unconditional absolute-address guarantee.
var scalarLoopEntry = parseScalarLoopEntry(os.Getenv("WAGO_AMD64_SCALAR_LOOP_ENTRY"))

func parseScalarLoopEntry(s string) int {
	switch s {
	case "", "off":
		return -1
	case "selective":
		return -2
	case "0":
		return 0
	case "8":
		return 8
	case "16":
		return 16
	case "24":
		return 24
	default:
		panic("WAGO_AMD64_SCALAR_LOOP_ENTRY must be off, selective, 0, 8, 16 or 24")
	}
}

type scalarLoopAlignmentBudget struct{ extraBytes uint8 }

// Construct the actual selected RSI pointer/cell/test/Jcc encoding once per
// compiler process, not per loop. This is verified against emitInterruptCheck
// by tests; no guessed universal poll length or live trap metadata is used.
var ordinaryScalarLoopPollBytes = scalarLoopPollEncodingBytes()

func scalarLoopPollEncodingBytes() int {
	var a x64.Asm
	a.Load64(RSI, RBX, -offTrapCellPtr)
	a.Load32(RSI, RSI, 0)
	a.TestSelf(RSI, false)
	a.JccPlaceholder(condNE)
	return a.Len()
}

func (f *fn) alignExperimentalScalarLoop(params, results int) bool {
	if !f.interruptible || scalarLoopEntry == -1 {
		return false
	}
	if scalarLoopEntry == -2 {
		if params != 0 || results != 0 || f.moduleEH || len(f.customInstructions) != 0 || interruptPollPaddingWidth() != 0 {
			f.stats.peep("scalar-entry-skip-policy")
			return false
		}
		x := f.a.Len() & 31
		oldPad := (16 - x) & 15
		if x >= 24 {
			oldPad = 40 - x
		}
		if f.compactLoopAlign32 {
			oldPad = (-x) & 31
		}
		oldHeader := (x + oldPad) & 31
		// Select the vulnerable actual entry shape only, not every loop.
		if oldHeader != 16 || (oldHeader+ordinaryScalarLoopPollBytes)&31 != 30 {
			f.stats.peep("scalar-entry-skip-shape")
			return false
		}
		pad := (-x) & 31
		extra := pad - oldPad
		if extra < 0 || int(f.extraBytes)+extra > 64 {
			f.stats.peep("scalar-entry-skip-budget")
			return false
		}
		f.extraBytes += uint8(extra)
		f.a.AlignLoop32()
		if diagnosticsEnabled && f.stats != nil {
			f.stats.peep("scalar-entry-selected")
			f.stats.Peephole["scalar-entry-padding-bytes"] += pad
			f.stats.Peephole["scalar-entry-incremental-bytes"] += extra
		}
		return true
	}
	// Original explicit offset modes remain diagnostics. Offset0 reuses the
	// existing efficient multi-byte emitter; all padding precedes controlSite.
	if scalarLoopEntry == 0 {
		f.a.AlignLoop32()
		return true
	}
	n := (scalarLoopEntry - f.a.Len()) & 31
	for ; n > 0; n-- {
		f.a.EmitBytes([]byte{0x90})
	}
	return true
}
