//go:build arm64

package arm64

import (
	"os"
	"strconv"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var inlineIntegerTuplesEnabled = true

// Explicit ordinary inliner limits continue to bound this class as well.
var inlineTupleMaxBytes = func() int {
	if v := os.Getenv("WAGO_INLINE_MAXBYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return min(n, 64)
		}
	}
	return 64
}()

func boundedIntegerTupleBody(code wasm.Func, facts inlineFacts, classifier wasm.ModuleInstructionClassifier) bool {
	if !facts.regABIIntOnly || facts.results != 2 || facts.params > 4 || facts.declaredLocals > 4 ||
		facts.params+facts.declaredLocals > 8 || facts.moduleEH || facts.hasControlCall || facts.calleeCount != 0 ||
		facts.touchesMem || facts.touchesGlobal || len(code.BodyBytes) == 0 || len(code.BodyBytes) > inlineTupleMaxBytes {
		return false
	}
	declared := uint64(0)
	for _, run := range code.Locals.Runs {
		if run.Type != wasm.I32 && run.Type != wasm.I64 {
			return false
		}
		declared += uint64(run.Count)
		if declared > 4 {
			return false
		}
	}
	if declared != uint64(facts.declaredLocals) {
		return false
	}
	r := wasm.NewReader(code.BodyBytes)
	depth := 0
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			if depth == 0 {
				return !r.HasNext()
			}
			depth--
			continue
		}
		// Structured numeric control and locals add no native calls or hidden
		// memory/global/reference effects to the caller's register frame.
		if !(op <= 0x05 || op == 0x0c || op == 0x0d || op == 0x0f || op >= 0x1a && op <= 0x1c ||
			op >= 0x20 && op <= 0x22 || op >= 0x41 && op <= 0xc4) || classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
		if op == 0x02 || op == 0x03 || op == 0x04 {
			if depth == 8 {
				return false
			}
			depth++
		}
	}
	return false
}

func (f inlineFacts) inlineIntegerTuple(policy CodegenPolicy) bool {
	return f.boundedTuple && !policy.CompactNative && policy.EnabledOption(optInlineIntegerTuples) && !profileEnabled
}
