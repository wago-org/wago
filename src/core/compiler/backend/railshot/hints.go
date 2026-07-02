package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Function pre-scan (OPTIMIZATIONS.md "FuncHints"): ONE walk of the decoded
// instruction AST collects every fact the compiler wants before emission —
// call/memory shape for model and pool gating, and loop-weighted hotness scores
// for register pinning. Uses inside loops score far higher since loops dominate
// runtime. When the AST is unavailable (a programmatically built module carrying
// only BodyBytes), all scores are zero and pinning falls back to the first-N
// integer locals.

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

// funcHints is everything scanBody yields.
type funcHints struct {
	hasCall       bool // any direct or indirect call
	callsSelf     bool // a direct call to the function's own index
	touchesMemory bool // any linear-memory op
	usesBulkMem   bool // memory.copy/fill (rep movs/stos clobber RDI/RSI/RCX)

	// Loop-weighted hotness: local.get/global.get = 1×, set/tee = 2×, ×loopWeight
	// per enclosing loop level.
	localScore  []int64
	globalScore []int64

	// globalElig[g]: global g is accessed inside a loop whose subtree contains NO
	// call. Value-pinning such a global in a call-making function is a win: the
	// per-iteration memory traffic disappears while the coherence spill/reload
	// lands only on the (sparse) calls outside that loop. The innermost enclosing
	// loop decides — if it calls, no outer loop can be call-free.
	globalElig []bool
}

// scanBody performs the single pre-scan walk. selfIdx is the function's global
// function index (for callsSelf).
func scanBody(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32) funcHints {
	h := funcHints{
		localScore:  make([]int64, nLocals),
		globalScore: make([]int64, nGlobals),
		globalElig:  make([]bool, nGlobals),
	}
	// walk returns whether the subtree contains a call. cur collects the globals
	// whose innermost enclosing loop is the currently open one (nil outside loops).
	var walk func(instrs []wasm.Instruction, depth int, cur *[]uint32) bool
	walk = func(instrs []wasm.Instruction, depth int, cur *[]uint32) bool {
		w := loopWeight(depth)
		sub := false
		for i := range instrs {
			in := &instrs[i]
			switch in.Kind {
			case wasm.InstrCall:
				sub, h.hasCall = true, true
				if in.Index == selfIdx {
					h.callsSelf = true
				}
			case wasm.InstrCallIndirect:
				sub, h.hasCall = true, true
			case wasm.InstrLocalGet:
				if int(in.Index) < nLocals {
					h.localScore[in.Index] += w
				}
			case wasm.InstrLocalSet, wasm.InstrLocalTee:
				if int(in.Index) < nLocals {
					h.localScore[in.Index] += 2 * w
				}
			case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
				if int(in.Index) < nGlobals {
					if in.Kind == wasm.InstrGlobalSet {
						h.globalScore[in.Index] += 2 * w
					} else {
						h.globalScore[in.Index] += w
					}
					if cur != nil {
						*cur = append(*cur, in.Index)
					}
				}
			case wasm.InstrLoop:
				var mine []uint32
				if walk(in.Body().Instrs, depth+1, &mine) {
					sub = true // call inside: its globals are not eligible
				} else {
					for _, g := range mine {
						h.globalElig[g] = true
					}
				}
			case wasm.InstrBlock:
				if walk(in.Body().Instrs, depth, cur) {
					sub = true
				}
			case wasm.InstrIf:
				if walk(in.Then(), depth, cur) {
					sub = true
				}
				if walk(in.Else(), depth, cur) {
					sub = true
				}
			case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
				h.usesBulkMem, h.touchesMemory = true, true
			default:
				if instrTouchesMemory(in.Kind) {
					h.touchesMemory = true
				}
			}
		}
		return sub
	}
	walk(body.Instrs, 0, nil)
	return h
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
