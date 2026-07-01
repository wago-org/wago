package x64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Local hotness scan (ported from backend/amd64 funchints): a single walk of the
// decoded instruction AST that scores each local by use, weighting uses inside
// loops far higher since loops dominate runtime. assignPinnedLocals pins the
// highest-scoring integer locals. When the AST is unavailable (a programmatically
// built module carrying only BodyBytes), all scores are zero and pinning falls
// back to the first-N integer locals.

const (
	loopWeightFactor   = 10
	maxLoopWeightDepth = 6
)

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

// localHotness returns per-local usage scores for the function body.
func localHotness(body wasm.Expr, nLocals int) []int64 {
	scores := make([]int64, nLocals)
	var walk func(instrs []wasm.Instruction, loopDepth int)
	add := func(local uint32, delta int64) {
		if int(local) < nLocals {
			scores[local] += delta
		}
	}
	walk = func(instrs []wasm.Instruction, loopDepth int) {
		w := loopWeight(loopDepth)
		for i := range instrs {
			in := &instrs[i]
			switch in.Kind {
			case wasm.InstrLocalGet:
				add(in.Index, w)
			case wasm.InstrLocalSet, wasm.InstrLocalTee:
				add(in.Index, 2*w)
			case wasm.InstrLoop:
				walk(in.Body().Instrs, loopDepth+1)
			case wasm.InstrBlock:
				walk(in.Body().Instrs, loopDepth)
			case wasm.InstrIf:
				walk(in.Then(), loopDepth)
				walk(in.Else(), loopDepth)
			}
		}
	}
	walk(body.Instrs, 0)
	return scores
}
