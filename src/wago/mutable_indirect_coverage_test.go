//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestMutableIndirectStaticCoverage is an upper bound for #922 admission.
// A module-level mutation or imported/exported table marks all its indirect
// sites potentially mutable; no dynamic target distribution is inferred.
func TestMutableIndirectStaticCoverage(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"corpus/workloads/semantic/yyjson/yyjson.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{})
			if err != nil {
				t.Fatal(err)
			}
			var indirect, refCalls, mutations int
			mutableBoundary := false
			for _, imp := range m.Imports {
				mutableBoundary = mutableBoundary || imp.Type.Kind == wasm.ExternTable
			}
			for _, exp := range m.Exports {
				mutableBoundary = mutableBoundary || exp.Index.Kind == wasm.ExternTable
			}
			classifier := wasm.NewModuleInstructionClassifier(m, true)
			var imm wasm.InstructionImmediate
			for _, fn := range m.Code {
				r := wasm.NewReader(fn.BodyBytes)
				for r.HasNext() {
					op, err := r.Byte()
					if err != nil {
						t.Fatal(err)
					}
					if err := classifier.ClassifyInto(r, op, &imm); err != nil {
						t.Fatal(err)
					}
					switch imm.Kind {
					case wasm.InstrCallIndirect:
						indirect++
					case wasm.InstrCallRef:
						refCalls++
					case wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableFill, wasm.InstrTableCopy, wasm.InstrTableInit:
						mutations++
					}
				}
			}
			t.Logf("tables=%d call_indirect_sites=%d call_ref_sites=%d table_mutation_ops=%d imported_or_exported_table=%v potentially_mutable_indirect_upper_bound=%d", len(m.Tables), indirect, refCalls, mutations, mutableBoundary, func() int {
				if mutations > 0 || mutableBoundary {
					return indirect
				}
				return 0
			}())
		})
	}
}

func TestMutableIndirectCorpusCoverage(t *testing.T) {
	root := filepath.Join("../..", "corpus/workloads")
	files, mutableModules, mutableSites := 0, 0, 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".wasm") {
			return nil
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{MultiMemory: true})
		if err != nil {
			return err
		}
		mutable := false
		for _, imp := range m.Imports {
			mutable = mutable || imp.Type.Kind == wasm.ExternTable
		}
		for _, exp := range m.Exports {
			mutable = mutable || exp.Index.Kind == wasm.ExternTable
		}
		classifier := wasm.NewModuleInstructionClassifier(m, true)
		var imm wasm.InstructionImmediate
		calls := 0
		for _, fn := range m.Code {
			r := wasm.NewReader(fn.BodyBytes)
			for r.HasNext() {
				op, err := r.Byte()
				if err != nil {
					return err
				}
				if err := classifier.ClassifyInto(r, op, &imm); err != nil {
					return err
				}
				switch imm.Kind {
				case wasm.InstrCallIndirect:
					calls++
				case wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableFill, wasm.InstrTableCopy, wasm.InstrTableInit:
					mutable = true
				}
			}
		}
		if mutable && calls > 0 {
			mutableModules++
			mutableSites += calls
			t.Logf("potential: %s sites=%d", strings.TrimPrefix(path, root+string(filepath.Separator)), calls)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("wasm_workload_files=%d potentially_mutable_indirect_modules=%d indirect_sites_upper_bound=%d", files, mutableModules, mutableSites)
}
