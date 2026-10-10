//go:build amd64

package amd64

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type leafPinCoverage struct{ functions, eligible, calls, notInline int }

func measureLeafPinCoverage(m *wasm.Module) (leafPinCoverage, error) {
	hints, _, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount(), nil, false)
	if err != nil {
		return leafPinCoverage{}, err
	}
	inline := buildInlineTargets(m, hints, currentCodegenPolicy())
	result := leafPinCoverage{functions: len(m.Code)}
	for i, h := range hints {
		ft, ok := m.LocalFuncType(i)
		if !ok || !sigFitsRegABI(ft) || !sigIsIntOnly(ft) || int(h.localCount) != len(ft.Params) ||
			h.flags.has(hintHasCall|hintTouchesMemory|hintUsesBulkMem|hintHasTailCall|hintHasSIMD|hintModuleEH) || h.globalCount != 0 {
			continue
		}
		result.eligible++
		n := int(h.inlineCallSiteCount())
		result.calls += n
		if inline.target(m.ImportedFuncCount()+i) == nil {
			result.notInline += n
		}
	}
	return result, nil
}

func TestLeafPinCoverageControl(t *testing.T) {
	// 180 NOPs keep an otherwise pure integer leaf out of the inliner.
	leaf := append([]byte{0, 0x20, 0}, make([]byte, 180)...)
	for i := 3; i < len(leaf); i++ {
		leaf[i] = 0x01
	}
	leaf = append(leaf, 0x0b)
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x10, 1, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
	)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	got, err := measureLeafPinCoverage(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.notInline != 1 {
		t.Fatalf("non-inlined eligible calls = %d, want 1", got.notInline)
	}
}

// TestLeafPinCoverageCorpus is a reproducible static coverage control for #913.
// The count is an upper bound: it does not assume every remaining call has a
// live pinned local, nor that a function marked inlineable is always inlined.
func TestLeafPinCoverageCorpus(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"corpus/workloads/applications/brotli/brotli.wasm",
		"bench/startup/twins/json-as.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../../../../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := frontend.DecodeValidate(data)
			if err != nil {
				t.Fatal(err)
			}
			got, err := measureLeafPinCoverage(m)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("functions=%d eligible=%d incoming_call_sites=%d non_inlineable_target_sites=%d", got.functions, got.eligible, got.calls, got.notInline)
		})
	}
}
