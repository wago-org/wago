//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

const sharedGrowLargeIndexPath = "constant"

func sharedGrowLargeIndexConfig(t testing.TB, wide bool) *RuntimeConfig {
	t.Helper()
	return sharedGrowFailureConfig(t, wide).WithMaxMemoriesPerModule(MaxMemoriesPerModuleLimit)
}

// Repeated imported indexes alias one empty backing. Only the selected shared
// backing has a page, so execution storage does not increase with the index.
func sharedGrowLargeIndexModule(wide bool, index uint32, imported bool) []byte {
	typ, flags := wasm.I32, byte(3)
	if wide {
		typ, flags = wasm.I64, 7
	}
	memories, imports := make([][]byte, index+1), make([][]byte, index+1)
	for i := range memories {
		name, memory := "other", []byte{1, 0, 0}
		if uint32(i) == index {
			name, memory = "selected", []byte{flags, 1, 1}
		}
		memories[i] = memory
		entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		imports[i] = append(append(entry, 2), memory...)
	}
	params, body := []wasm.ValType{typ}, []byte{0x20, 0}
	if sharedGrowLargeIndexPath == "constant" {
		params, body = nil, []byte{0x41, 1}
		if wide {
			body[0] = 0x42
		}
	}
	body = append(body, 0x40)
	body = append(body, wasmtest.ULEB(index)...)
	body = append(body, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{typ})))}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	return wasmtest.Module(sections...)
}

func sharedGrowLargeIndexInstance(t *testing.T, cfg *RuntimeConfig, c *Compiled, wide bool) *Instance {
	t.Helper()
	other, err := NewMemory(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	flags := byte(3)
	if wide {
		flags = 7
	}
	pc, err := cfg.Compile(wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{flags, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0)))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	provider, err := Instantiate(pc)
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
	provider.Close() // The consumer retains the backing after its owner closes.
	return in
}

func sharedGrowLargeIndexInvoke(t *testing.T, in *Instance, wide bool, delta uint64) {
	t.Helper()
	args, want := []uint64{delta}, uint64(1)
	if sharedGrowLargeIndexPath == "constant" {
		args, delta = nil, 1
	}
	if delta != 0 {
		want = uint64(^uint32(0))
		if wide {
			want = ^uint64(0)
		}
	}
	if got, err := in.Invoke("run", args...); err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("delta%d: %x, %v; want%x", delta, got, err, want)
	}
}

func TestSharedGrowLargeMemoryIndexCompile(t *testing.T) {
	for _, index := range []uint32{1365, 1366, 4095} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("index%d/wide%t", index, wide), func(t *testing.T) {
				c, err := sharedGrowLargeIndexConfig(t, wide).Compile(sharedGrowLargeIndexModule(wide, index, false))
				if err != nil {
					t.Fatal(err)
				}
				c.Close()
			})
		}
	}
}
func TestSharedGrowLargeMemoryIndexImportedResults(t *testing.T) {
	for _, index := range []uint32{1365, 1366, 4095} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("index%d/wide%t", index, wide), func(t *testing.T) {
				cfg := sharedGrowLargeIndexConfig(t, wide)
				c, err := cfg.Compile(sharedGrowLargeIndexModule(wide, index, true))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { c.Close() })
				in := sharedGrowLargeIndexInstance(t, cfg, c, wide)
				sharedGrowLargeIndexInvoke(t, in, wide, 0)
				sharedGrowLargeIndexInvoke(t, in, wide, 1)
			})
		}
	}
}
