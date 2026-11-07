//go:build wago_profile && ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func indexedWaitLargeImportedModule(address64 bool, index uint32, sub byte) []byte {
	address, expected, limits, align := wasm.I32, wasm.I32, byte(3), byte(2)
	if address64 {
		address, limits = wasm.I64, 7
	}
	if sub == 2 {
		expected, align = wasm.I64, 3
	}
	imports := make([][]byte, index+1)
	for i := range imports {
		name, memory := "other", []byte{1, 0, 1}
		if uint32(i) == index {
			name, memory = "selected", []byte{limits, 1, 1}
		}
		entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		imports[i] = append(append(entry, 2), memory...)
	}
	body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, sub, align | 0x40}
	body = append(body, wasmtest.ULEB(index)...)
	body = append(body, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{address, expected, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

// Many declared indexes alias one small unshared backing; the selected shared
// memory has one page. This bounds execution memory independently of index.
func indexedWaitLargeImportedInstance(t *testing.T, cfg *RuntimeConfig, c *Compiled, address64 bool) *Instance {
	t.Helper()
	other, err := NewMemory(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	limits := byte(3)
	if address64 {
		limits = 7
	}
	providerCode, err := cfg.Compile(wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{limits, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0)))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { providerCode.Close() })
	provider, err := Instantiate(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { provider.Close() })
	selected, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: NewImports().Memory("env", "other", other).Memory("env", "selected", selected)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	provider.Close()
	return in
}

func TestIndexedAtomicWaitLargeImportedHelperEntries(t *testing.T) {
	for _, index := range []uint32{1365, 1366, 4095} {
		for _, address64 := range []bool{false, true} {
			for _, sub := range []byte{1, 2} {
				t.Run(fmt.Sprintf("index%d/addr64%t/op%d", index, address64, sub), func(t *testing.T) {
					defer func() {
						if recovered := recover(); recovered != nil {
							t.Errorf("valid indexed wait failed: %v", recovered)
						}
					}()
					trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
					t.Cleanup(func() { trace.Close() })
					cfg := indexedWaitConfig().WithMaxMemoriesPerModule(MaxMemoriesPerModuleLimit)
					c, err := cfg.WithCodeProfile(trace).Compile(indexedWaitLargeImportedModule(address64, index, sub))
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { c.Close() })
					in := indexedWaitLargeImportedInstance(t, cfg, c, address64)
					helper := int(shared.AtomicHelperWait32)
					if sub == 2 {
						helper = int(shared.AtomicHelperWait64)
					}
					count := func() int {
						n := 0
						for _, span := range trace.Spans() {
							if span.Kind == "atomic-wait-helper" && span.Function == helper {
								n++
							}
						}
						return n
					}
					for _, value := range []uint64{1, 0} {
						before := count()
						got, err := in.Invoke("run", 0, value, 0)
						if err != nil || len(got) != 1 || got[0] != 2-value {
							t.Fatalf("value%d:%x,%v", value, got, err)
						}
						want := 1 - int(value)
						if entries := count() - before; entries != want {
							t.Fatalf("large-index helper entries%d,want%d", entries, want)
						}
						t.Logf("large-index value%d helper entries%d", value, count()-before)
					}
				})
			}
		}
	}
}
