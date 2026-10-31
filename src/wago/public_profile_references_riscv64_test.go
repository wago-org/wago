//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_profile

package wago

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ProfileReferenceAndTailExecution(t *testing.T) {
	for _, kind := range []string{"scalar-tail", "reference-call", "reference-tail"} {
		for _, vector := range []bool{false, true} {
			for _, artifact := range []bool{false} {
				t.Run(fmt.Sprintf("%s/vector%t/artifact%t", kind, vector, artifact), func(t *testing.T) {
					types := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
					functions := []byte{2, 0, 0}
					bodies := [][]byte{wasmtest.Code([]byte{0x12, 1, 0x0b}), wasmtest.Code([]byte{0x41, 42, 0x0b})}
					export, want := uint32(0), uint64(42)
					var declaration []byte
					if kind != "scalar-tail" {
						types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))
						op := byte(0x14)
						if kind == "reference-tail" {
							op = 0x15
						}
						functions = []byte{3, 1, 1, 0}
						bodies = [][]byte{wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd2, 0, op, 1, 0x0b}), wasmtest.Code([]byte{0xd0, 0x70, 0x10, 1, 0xd1, 0x0b})}
						declaration = wasmtest.Section(9, []byte{1, 3, 0, 1, 0})
						export, want = 2, 1
					}
					if vector {
						types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
					}
					sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, functions), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, export)))}
					if declaration != nil {
						sections = append(sections, declaration)
					}
					sections = append(sections, wasmtest.Section(10, wasmtest.Vec(bodies...)))
					profile := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true, UnwindMaps: true})
					defer profile.Close()
					c, err := NewRuntimeConfig().WithCodeProfile(profile).Compile(wasmtest.Module(sections...))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()

					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					events, status := profile.Read(0)
					if len(events) == 0 || status.Dropped != 0 {
						t.Fatal("missing profile image", events, status)
					}
					image := events[len(events)-1].Image
					if image.Size != uint64(len(c.code)) || !bytes.Equal(image.Code, c.code) {
						t.Fatal("profile does not describe the executed image")
					}
					for _, err := range []error{jitprofile.ValidateRegions(image.Regions, image.Size), jitprofile.ValidateSources(image.Sources, image.Size), jitprofile.ValidateUnwind(image.Unwind, image.Size)} {
						if err != nil {
							t.Fatal(err)
						}
					}
					if (!artifact && (len(image.Unwind) == 0 || len(image.Sources) == 0)) || (artifact && (len(image.Unwind) != 0 || len(image.Sources) != 0 || len(image.Functions) != 0)) {
						t.Fatal("inaccurate profile provenance")
					}
					if got, err := in.Invoke("run"); err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal(got, err)
					}
				})
			}
		}
	}
}
