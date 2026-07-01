package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// funcHints is a cheap, single-walk summary of a function body used to make
// backend setup decisions — currently which locals to pin to registers —
// without a full IR pass. It is purely advisory: it is derived from the decoded
// instruction AST and nothing here affects correctness, so any inaccuracy only
// changes which values get pinned, never the generated semantics. When the AST
// is unavailable (e.g. a programmatically built module that carries only
// BodyBytes), the scan simply yields zero scores and callers fall back to the
// legacy first-N pinning.
type funcHints struct {
	localScore   []int64 // per-local hotness, weighted by enclosing loop depth
	callCount    int     // direct + indirect calls (raw count)
	callWeight   int64   // calls weighted by enclosing loop depth (spill-tax basis)
	loopDepthMax int     // deepest loop nesting encountered
	touchesMem   bool    // body has a load/store or global.get/set (memory-base pin candidate)
	scanned      bool    // a non-empty decoded body was walked (else: no usage data)
}

// isMemOrGlobalKind reports whether an instruction reads or writes linear memory
// or a global — i.e. codegen that indexes off the linear-memory base and so
// benefits from memory-base pinning. This is advisory only: a miss just leaves a
// function on the reload path (see cg.pinMemBase), never a wrong result.
func isMemOrGlobalKind(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
		return true
	}
	// The load/store opcodes form one contiguous run in the InstrKind enum.
	return k >= wasm.InstrI32Load && k <= wasm.InstrI64Store32
}

// loopWeightFactor multiplies a local's per-use contribution for each enclosing
// loop level: loops dominate runtime, so a local touched once inside a nested
// loop should outrank one touched many times in straight-line code.
const loopWeightFactor = 10

// maxLoopWeightDepth caps the loop multiplier so deeply nested loops cannot
// overflow the score accumulator.
const maxLoopWeightDepth = 6

// loopWeight is loopWeightFactor ** min(depth, maxLoopWeightDepth).
func loopWeight(depth int) int64 {
	if depth > maxLoopWeightDepth {
		depth = maxLoopWeightDepth
	}
	w := int64(1)
	for i := 0; i < depth; i++ {
		w *= loopWeightFactor
	}
	return w
}

// scanHints walks the decoded body once and returns its hints. nLocals sizes the
// per-local score table; out-of-range indices (there should be none in valid
// wasm) are ignored.
func scanHints(body wasm.Expr, nLocals int) funcHints {
	h := funcHints{localScore: make([]int64, nLocals), scanned: len(body.Instrs) > 0}
	h.walk(body.Instrs, 0)
	return h
}

func (h *funcHints) addScore(local uint32, delta int64) {
	if int(local) < len(h.localScore) {
		h.localScore[local] += delta
	}
}

func (h *funcHints) walk(instrs []wasm.Instruction, loopDepth int) {
	w := loopWeight(loopDepth)
	for i := range instrs {
		in := &instrs[i]
		if isMemOrGlobalKind(in.Kind) {
			h.touchesMem = true
		}
		switch in.Kind {
		case wasm.InstrLocalGet:
			h.addScore(in.Index, w)
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			h.addScore(in.Index, 2*w)
		case wasm.InstrCall, wasm.InstrCallIndirect:
			h.callCount++
			h.callWeight += w
		case wasm.InstrLoop:
			d := loopDepth + 1
			if d > h.loopDepthMax {
				h.loopDepthMax = d
			}
			h.walk(in.Body().Instrs, d)
		case wasm.InstrBlock:
			h.walk(in.Body().Instrs, loopDepth)
		case wasm.InstrIf:
			h.walk(in.Then(), loopDepth)
			h.walk(in.Else(), loopDepth)
		}
	}
}
