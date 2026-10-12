//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestScopedLoopConstRejectsBulkWithinScope(t *testing.T) {
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 1, loopIntConst: [4]int64{0x1234567}}
	h.flags |= hintUsesBulkMem
	module := modMem(t, 1, nil, nil, []byte{0, 0x0b})
	classifier := wasm.NewModuleInstructionClassifier(module, true)
	prefix := append([]byte{0x41}, wasmtest.SLEB32(0x1234567)...)
	prefix = append(prefix, 0x6c, 0x1a)
	for _, bulk := range [][]byte{
		{0xfc, 8, 0, 0}, {0xfc, 9, 0}, {0xfc, 10, 0, 0}, {0xfc, 11, 0},
		{0xfc, 12, 0, 0}, {0xfc, 13, 0}, {0xfc, 14, 0, 0}, {0xfc, 17, 0},
	} {
		for _, inside := range []bool{false, true} {
			body := append([]byte{}, prefix...)
			if inside {
				body = append(body, bulk...)
			}
			body = append(body, 0x0b)
			if !inside {
				body = append(body, bulk...)
			}
			r := wasm.ReaderFrom(body)
			selected := scopedConstHintsFrom(&h)
			uses := scopedLoopConstUses{hints: &selected}
			_, calls := scanLoopSetLocals(&r, classifier, nil, &uses)
			if calls || r.Offset() != 0 {
				t.Fatal("scanner lost call/reader proof")
			}
			if inside {
				if !uses.blocked || uses.mask != 0 {
					t.Fatal("bulk loop admitted immutable lease")
				}
			} else if uses.blocked || uses.mask != 1 {
				t.Fatal("bulk after loop prevented local lease")
			}
		}
	}
}

func TestScopedLoopConstAllowsBulkOutsideScope(t *testing.T) {
	h := funcHintView{loopIntConstCount: 1}
	h.flags |= hintUsesBulkMem
	f := fn{scopedConstHints: scopedConstHintsFrom(&h), policy: currentCodegenPolicy()}
	if _, ok := f.scopedLoopConstUses(); !ok {
		t.Fatal("function-level bulk scratch prevented a loop-local proof")
	}
}
