//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

// Optional admission diagnostics; no production workload recognition.
func TestScopedConstCorpusLoops(t *testing.T) {
	path := os.Getenv("WAGO_CONST_PROBE")
	if path == "" {
		t.Skip("set a diagnostic module path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if diagnosticsEnabled {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats}); err != nil {
			t.Fatal(err)
		}
		for _, s := range stats.Funcs {
			t.Logf("compiled function %d pinned=%d spills=%d reloads=%d constant leases=%d page leases=%d scoped leases=%d", s.FuncIdx, s.PinnedLocals, s.Spills, s.Reloads, s.Peephole["loop-int-const"], s.Peephole["loop-memory-base"], s.Peephole["scoped-loop-int-const"])
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i, body := range m.Code {
		h, err := scanFuncBody(body, 256, len(m.Globals), uint32(i), nil, m)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("function %d bulk=%v tableMutation=%v types=%x literals=%x", i, h.flags.has(hintUsesBulkMem), h.flags.has(hintMutatesTable), h.loopIntConstTypes, h.loopIntConst)
		r := wasm.ReaderFrom(body.BodyBytes)
		var imm wasm.InstructionImmediate
		loops, callfree, matched := 0, 0, 0
		for r.Offset() < len(body.BodyBytes) {
			op, err := r.Byte()
			if err != nil {
				t.Fatal(err)
			}
			if err = classifier.ClassifyInto(&r, op, &imm); err != nil {
				t.Fatal(err)
			}
			if op != 0x03 {
				continue
			}
			loops++
			selected := scopedConstHintsFrom(&h)
			uses := scopedLoopConstUses{hints: &selected}
			_, calls := scanLoopSetLocals(&r, classifier, nil, &uses)
			if !calls {
				callfree++
				if uses.mask != 0 {
					matched++
					t.Logf("function %d loop at %d literals mask=%x", i, r.Offset(), uses.mask)
				}
			}
		}
		t.Logf("function %d loops=%d callfree=%d matched=%d", i, loops, callfree, matched)
	}
}
