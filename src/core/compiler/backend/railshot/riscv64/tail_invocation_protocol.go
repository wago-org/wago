//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func (e *tailRAEmitter) recordBytes() int {
	if e.invocationContext {
		return tailRAInvocationRecordBytes
	}
	return tailRARecordBytes
}

// The prologue/selector authenticated the complete activation view. Proper
// tails retain these exact actual directions; Engine bounds alone never grant
// authority to an arbitrary native stack span.
func (e *tailRAEmitter) invocationTransport(pointer enc.Reg, bytes int, view enc.Reg, direction, capacity int32) {
	a := &e.a
	a.Ld(enc.X29, view, direction)
	e.fail(enc.NE, pointer, enc.X29)
	a.Ld(enc.X29, view, capacity)
	a.Constant64(enc.Scratch0, int64(bytes))
	e.fail(enc.LTU, enc.X29, enc.Scratch0)
	a.Constant64(enc.Scratch0, nativePublicImportTransportBytes)
	e.fail(enc.LTU, enc.Scratch0, enc.X29)
	a.Add(enc.Scratch0, pointer, enc.X29)
	e.fail(enc.LTU, enc.Scratch0, pointer)
	if bytes != 0 {
		tailRAMask(a, enc.X29, pointer, 7)
		e.fail(enc.NE, enc.X29, enc.Zero)
		e.fail(enc.EQ, pointer, enc.Zero)
	}
}

func (e *tailRAEmitter) transportBytes() int {
	if e.invocationContext {
		return nativePublicImportTransportBytes
	}
	return nativeImportTransportBytes
}
func (e *tailRAEmitter) resultsOffset() int {
	if e.invocationContext {
		return tailRAPublicResultsOffset
	}
	return tailRAResultsOffset
}
func (e *tailRAEmitter) viewOffset() int {
	if e.invocationContext {
		return tailRAPublicRecordBytes
	}
	return tailRARecordBytes
}
func (e *tailRAEmitter) maskOffset() int {
	if e.invocationContext {
		return tailRAPublicMaskOffset
	}
	return 112
}
func (e *tailRAEmitter) maskWords() int {
	if e.invocationContext {
		return 8
	}
	return 4
}
