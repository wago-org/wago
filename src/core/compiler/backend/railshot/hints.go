package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Local hotness scan (ported from backend/railshot/amd64 funchints): a single walk of the
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

// bodyHasCall reports whether the function makes any (direct or indirect) call.
// Call-free functions keep the always-in-register local model; call-making ones
// may use WARP's lazy STACK_REG spill model (store-dirty-around-call, lazy
// reload), depending on bodyUseStackReg.
func bodyHasCall(body wasm.Expr) bool {
	var walk func(instrs []wasm.Instruction) bool
	walk = func(instrs []wasm.Instruction) bool {
		for i := range instrs {
			in := &instrs[i]
			switch in.Kind {
			case wasm.InstrCall, wasm.InstrCallIndirect:
				return true
			case wasm.InstrLoop, wasm.InstrBlock:
				if walk(in.Body().Instrs) {
					return true
				}
			case wasm.InstrIf:
				if walk(in.Then()) || walk(in.Else()) {
					return true
				}
			}
		}
		return false
	}
	return walk(body.Instrs)
}

func bodyCalls(body wasm.Expr, idx uint32) bool {
	var walk func(instrs []wasm.Instruction) bool
	walk = func(instrs []wasm.Instruction) bool {
		for i := range instrs {
			in := &instrs[i]
			switch in.Kind {
			case wasm.InstrCall:
				if in.Index == idx {
					return true
				}
			case wasm.InstrLoop, wasm.InstrBlock:
				if walk(in.Body().Instrs) {
					return true
				}
			case wasm.InstrIf:
				if walk(in.Then()) || walk(in.Else()) {
					return true
				}
			}
		}
		return false
	}
	return walk(body.Instrs)
}

// bodyTouchesMemory reports whether the function executes linear-memory ops.
// In guard-mode call+memory code the eager spill/reload model benchmarks faster
// than STACK_REG: it leaves more registers available to the memory/address/value
// path instead of reserving pinned-local registers that are repeatedly marked
// clobbered by calls.
// globalHotness scores each global by loop-weighted reference count (mirrors
// localHotness): global.get = 1×, global.set = 2×, ×loopWeight per loop level. Used
// to pick which globals' cell pointers to pin in registers.
func globalHotness(body wasm.Expr, nGlobals int) []int64 {
	scores := make([]int64, nGlobals)
	add := func(g uint32, delta int64) {
		if int(g) < nGlobals {
			scores[g] += delta
		}
	}
	var walk func(instrs []wasm.Instruction, loopDepth int)
	walk = func(instrs []wasm.Instruction, loopDepth int) {
		w := loopWeight(loopDepth)
		for i := range instrs {
			in := &instrs[i]
			switch in.Kind {
			case wasm.InstrGlobalGet:
				add(in.Index, w)
			case wasm.InstrGlobalSet:
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

// bodyUsesBulkMem reports whether the body contains memory.copy/fill, which lower
// to `rep movs`/`stos` and hard-clobber RDI/RSI/RCX — so those registers can't hold
// pinned locals in a function that uses them.
func bodyUsesBulkMem(body wasm.Expr) bool {
	var walk func(instrs []wasm.Instruction) bool
	walk = func(instrs []wasm.Instruction) bool {
		for i := range instrs {
			in := &instrs[i]
			if in.Kind == wasm.InstrMemoryCopy || in.Kind == wasm.InstrMemoryFill {
				return true
			}
			switch in.Kind {
			case wasm.InstrLoop, wasm.InstrBlock:
				if walk(in.Body().Instrs) {
					return true
				}
			case wasm.InstrIf:
				if walk(in.Then()) || walk(in.Else()) {
					return true
				}
			}
		}
		return false
	}
	return walk(body.Instrs)
}

func bodyTouchesMemory(body wasm.Expr) bool {
	var walk func(instrs []wasm.Instruction) bool
	walk = func(instrs []wasm.Instruction) bool {
		for i := range instrs {
			in := &instrs[i]
			if instrTouchesMemory(in.Kind) {
				return true
			}
			switch in.Kind {
			case wasm.InstrLoop, wasm.InstrBlock:
				if walk(in.Body().Instrs) {
					return true
				}
			case wasm.InstrIf:
				if walk(in.Then()) || walk(in.Else()) {
					return true
				}
			}
		}
		return false
	}
	return walk(body.Instrs)
}

func instrTouchesMemory(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
		wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
		wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U,
		wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
		wasm.InstrI32Store8, wasm.InstrI32Store16, wasm.InstrI64Store8, wasm.InstrI64Store16,
		wasm.InstrI64Store32,
		wasm.InstrMemorySize, wasm.InstrMemoryGrow, wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
		return true
	default:
		return false
	}
}

// bodyUseStackReg mirrors the f.usesCalls gate (compile.go): the lazy STACK_REG
// pinned-local model is used only for call functions that do NOT touch memory.
// guardMode is retained for the signature but no longer affects the decision —
// memory-touching functions are excluded in both modes (see compile.go).
func bodyUseStackReg(body wasm.Expr, guardMode bool) bool {
	_ = guardMode
	return bodyHasCall(body) && !bodyTouchesMemory(body) && !noStackReg
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
