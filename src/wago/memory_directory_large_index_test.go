//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !race

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func largeMemoryDirectoryModule(index uint32, wide, shared bool, operation string) []byte {
	address, constant, limits := wasm.I32, byte(0x41), byte(1)
	if wide {
		address, constant, limits = wasm.I64, 0x42, limits|4
	}
	if shared {
		limits |= 2
	}
	imports := make([][]byte, index+1)
	for i := range imports {
		name, memory := "other", []byte{1, 0, 2}
		if uint32(i) == index {
			name, memory = "selected", []byte{limits, 1, 2}
		}
		entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		imports[i] = append(append(entry, 2), memory...)
	}
	selected := wasmtest.ULEB(index)
	body := []byte{}
	result := wasm.I32
	local := func(i byte) { body = append(body, 0x20, i) }
	value := func(i byte) {
		local(i)
		if wide {
			body = append(body, 0xa7)
		}
	}
	memarg := func(op ...byte) {
		body = append(body, op...)
		body = append(body, 0x42)
		body = append(body, selected...)
		body = append(body, 0)
	}
	switch operation {
	case "size":
		body = append(body, 0x3f)
		body = append(body, selected...)
		result = address
	case "grow-zero", "grow-dynamic":
		if operation == "grow-zero" {
			body = append(body, constant, 0)
		} else {
			local(0)
		}
		body = append(body, 0x40)
		body = append(body, selected...)
		result = address
	case "load", "load-constant":
		if operation == "load-constant" {
			body = append(body, constant, 8)
		} else {
			local(0)
		}
		memarg(0x28)
	case "store":
		local(0)
		value(1)
		memarg(0x36)
		body = append(body, 0x41, 7)
	case "fill", "fill-constant":
		local(0)
		value(1)
		if operation == "fill-constant" {
			body = append(body, constant, 4)
		} else {
			local(2)
		}
		body = append(body, 0xfc, 11)
		body = append(body, selected...)
		body = append(body, 0x41, 7)
	case "copy", "copy-constant":
		local(0)
		local(1)
		if operation == "copy-constant" {
			body = append(body, constant, 4)
		} else {
			local(2)
		}
		body = append(body, 0xfc, 10)
		body = append(body, selected...)
		body = append(body, selected...)
		body = append(body, 0x41, 7)
	case "notify":
		local(0)
		value(1)
		memarg(0xfe, 0)
	default:
		panic("unknown directory operation")
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{address, address, address}, []wasm.ValType{result}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("memory", 2, index))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestLargeMemoryDirectoryIndexes(t *testing.T) {
	// Scaled32 directory fields reach their limit before scaled64 base loads.
	for _, index := range []uint32{1, 681, 682, 1365, 1366, 4095} {
		for _, wide := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, operation := range []string{"size", "grow-zero", "grow-dynamic", "load", "load-constant", "store", "fill", "fill-constant", "copy", "copy-constant", "notify"} {
					t.Run(fmt.Sprintf("index%d/wide%t/shared%t/%s", index, wide, shared, operation), func(t *testing.T) {
						defer func() {
							if v := recover(); v != nil {
								t.Errorf("valid indexed memory failed: %v", v)
							}
						}()
						cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithMaxMemoriesPerModule(MaxMemoriesPerModuleLimit)
						c, err := cfg.Compile(largeMemoryDirectoryModule(index, wide, shared, operation))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						limits := byte(1)
						if wide {
							limits |= 4
						}
						if shared {
							limits |= 2
						}
						providerCode, err := cfg.Compile(wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{limits, 1, 2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0)))))
						if err != nil {
							t.Fatal(err)
						}
						defer providerCode.Close()
						provider, err := Instantiate(providerCode)
						if err != nil {
							t.Fatal(err)
						}
						defer provider.Close()
						memory, err := provider.ExportedMemory("memory")
						if err != nil {
							t.Fatal(err)
						}
						other, err := NewMemory(0, 2)
						if err != nil {
							t.Fatal(err)
						}
						defer other.Close()
						in, err := Instantiate(c, InstantiateOptions{Imports: NewImports().Memory("env", "selected", memory).Memory("env", "other", other)})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						memory, err = in.ExportedMemory("memory")
						if err != nil {
							t.Fatal(err)
						}
						copy(memory.UnsafeBytes()[8:], []byte{0x44, 0x33, 0x22, 0x11})
						args := []uint64{8, 0x11223344, 4}
						want := uint64(7)
						switch operation {
						case "size", "grow-zero":
							args = []uint64{0, 0, 0}
							want = 1
						case "grow-dynamic":
							args = []uint64{1, 0, 0}
							want = 1
						case "load", "load-constant":
							want = 0x11223344
						case "copy", "copy-constant":
							args = []uint64{16, 8, 4}
						case "notify":
							args = []uint64{8, 0, 0}
							want = 0
						}
						got, err := in.Invoke("run", args...)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("result%v,%v; want%d", got, err, want)
						}
						if operation == "fill" || operation == "fill-constant" {
							if !bytes.Equal(memory.UnsafeBytes()[8:12], []byte{0x44, 0x44, 0x44, 0x44}) {
								t.Fatal("wrong fill destination")
							}
						}
						if operation == "copy" || operation == "copy-constant" {
							if !bytes.Equal(memory.UnsafeBytes()[16:20], []byte{0x44, 0x33, 0x22, 0x11}) {
								t.Fatal("wrong copy destination")
							}
						}
						if operation == "grow-dynamic" {
							if len(memory.UnsafeBytes()) != 2*65536 {
								t.Fatal("growth did not publish two pages")
							}
						}
						switch operation {
						case "load", "store", "fill", "fill-constant", "copy", "copy-constant", "notify":
							_, err = in.Invoke("run", 65536, 8, 4)
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
								t.Fatalf("bounds trap:%v", err)
							}
						}
						if operation == "fill" || operation == "copy" {
							if _, err = in.Invoke("run", 65536, 0, 0); err != nil {
								t.Fatalf("empty operation at end:%v", err)
							}
							_, err = in.Invoke("run", 65537, 0, 0)
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
								t.Fatalf("empty operation past end:%v", err)
							}
						}
					})
				}
			}
		}
	}
}
