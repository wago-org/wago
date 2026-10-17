//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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
		loads, err := memory64ScalarLoadCount(m)
		if err != nil {
			return err
		}
		scalarLoads += loads
		t.Logf("memory64 workload: %s", strings.TrimPrefix(path, root+string(filepath.Separator)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("wasm_workload_files=%d memory64_files=%d functions_in_memory64_modules=%d memory64_scalar_loads=%d", files, memory64, funcs, scalarLoads)
}

func memory64ScalarLoadCount(m *wasm.Module) (int, error) {
	loads := 0
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var imm wasm.InstructionImmediate
	for _, fn := range m.Code {
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return 0, err
			}
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				return 0, err
			}
			if op >= 0x28 && op <= 0x35 {
				// The classifier uses index zero for an implicit memarg. Resolve
				// imports and locals before attributing a load to memory64.
				memory, ok := m.MemoryType(imm.MemIndex)
				if !ok {
					return 0, fmt.Errorf("scalar load references unknown memory %d", imm.MemIndex)
				}
				if memory.Limits.Addr64 {
					loads++
				}
			}
		}
	}
	return loads, nil
}

func TestMemory64ScalarLoadCountMixedMemories(t *testing.T) {
	for imported := 0; imported <= 2; imported++ {
		for _, first64 := range []bool{false, true} {
			t.Run(fmt.Sprintf("imported=%d/first64=%t", imported, first64), func(t *testing.T) {
				memories := [][]byte{{0, 1}, {4, 1}}
				want := 1
				if first64 {
					memories[0], memories[1] = memories[1], memories[0]
					want = 2
				}
				sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil)))}
				if imported != 0 {
					var entries [][]byte
					for i := 0; i < imported; i++ {
						entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", i))...)
						entry = append(entry, 2)
						entries = append(entries, append(entry, memories[i]...))
					}
					sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
				}
				sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0})))
				if imported != 2 {
					sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories[imported:]...)))
				}
				// Two memory-zero loads use implicit and explicit indices. A third
				// load selects memory one; imported memories precede local memories.
				var body []byte
				for _, load := range []struct {
					index    byte
					explicit bool
				}{{0, false}, {0, true}, {1, true}} {
					address := byte(0x41)
					if memories[load.index][0] == 4 {
						address = 0x42
					}
					body = append(body, address, 0, 0x28)
					if load.explicit {
						body = append(body, 0x42, load.index)
					} else {
						body = append(body, 2)
					}
					body = append(body, 0, 0x1a)
				}
				body = append(body, 0x0b)
				sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				features := wasm.ValidationFeatures{MultiMemory: true}
				m, err := wasm.DecodeModuleWithFeatures(wasmtest.Module(sections...), features)
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModuleWithFeatures(m, features); err != nil {
					t.Fatal(err)
				}
				got, err := memory64ScalarLoadCount(m)
				if err != nil || got != want {
					t.Fatalf("memory64 scalar loads = %d, %v; want %d", got, err, want)
				}
			})
		}
	}
}
