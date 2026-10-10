//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestMemory64WorkloadCoverage checks whether the shipped real workload corpus
// contains any memory64 program before changing the AMD64 proof machinery.
func TestMemory64WorkloadCoverage(t *testing.T) {
	control, err := os.ReadFile(filepath.Join("../..", "tests/corpus/regressions/wasmtime-core3/core/memory64/codegen/commands.0.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := wasm.DecodeModuleWithFeatures(control, wasm.ValidationFeatures{MultiMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	control64 := false
	for _, mem := range cm.Memories {
		control64 = control64 || mem.Limits.Addr64
	}
	for _, imp := range cm.Imports {
		if imp.Type.Kind == wasm.ExternMem {
			control64 = control64 || imp.Type.MemType().Limits.Addr64
		}
	}
	if !control64 {
		t.Fatal("memory64 control fixture was not recognized")
	}
	root := filepath.Join("../..", "corpus/workloads")
	var files, memory64, funcs, scalarLoads int
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
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
		addr64 := false
		for _, mem := range m.Memories {
			addr64 = addr64 || mem.Limits.Addr64
		}
		for _, imp := range m.Imports {
			if imp.Type.Kind == wasm.ExternMem {
				addr64 = addr64 || imp.Type.MemType().Limits.Addr64
			}
		}
		if !addr64 {
			return nil
		}
		memory64++
		funcs += len(m.Code)
		classifier := wasm.NewModuleInstructionClassifier(m, true)
		var imm wasm.InstructionImmediate
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
				if op >= 0x28 && op <= 0x35 {
					scalarLoads++
				}
			}
		}
		t.Logf("memory64 workload: %s", strings.TrimPrefix(path, root+string(filepath.Separator)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("wasm_workload_files=%d memory64_files=%d memory64_functions=%d memory64_scalar_loads=%d", files, memory64, funcs, scalarLoads)
}
