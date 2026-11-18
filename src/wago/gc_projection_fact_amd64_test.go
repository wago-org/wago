//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep the container behind a global so its cast is dynamic. Its projected
// value has the same broad static type, but a different runtime type.
func gcProjectionFactModule(array bool, value []byte, use string) []byte {
	container := []byte{0x4f, 0, 0x5f, 1, 0x6d, 0}
	construct := []byte{0xfb, 0, 0}
	project := []byte{0xfb, 2, 0, 0}
	if array {
		container = []byte{0x4f, 0, 0x5e, 0x6d, 0}
		construct = []byte{0x41, 1, 0xfb, 6, 0}
		project = []byte{0x41, 0, 0xfb, 11, 0}
	}
	init := append(append([]byte{}, value...), construct...)
	init = append(init, 0x24, 0, 0x0b)
	body := append([]byte{0x23, 0, 0xfb, 22, 0}, project...)
	switch use {
	case "set":
		body = append(body, 0x21, 0, 0x20, 0)
	case "tee":
		body = append(body, 0x22, 0, 0x1a, 0x20, 0)
	}
	body = append(body, 0xfb, 22, 0, 0x1a, 0x41, 42, 0x0b)
	body = append([]byte{1, 1, 0x6d}, body...)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(container, []byte{0x4f, 0, 0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6d, 1, 0xd0, 0x6d, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), code)),
	)
}

func TestGCFusedProjectionDoesNotKeepContainerFact(t *testing.T) {
	for _, facts := range []bool{false, true} {
		for _, array := range []bool{false, true} {
			for _, value := range []struct {
				name string
				code []byte
			}{
				{"null", []byte{0xd0, 0x6d}}, {"i31", []byte{0x41, 1, 0xfb, 28}}, {"other-struct", []byte{0x41, 7, 0xfb, 0, 1}},
			} {
				for _, use := range []string{"immediate", "set", "tee"} {
					t.Run(fmt.Sprintf("facts%t/array%t/%s/%s", facts, array, value.name, use), func(t *testing.T) {
						c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1).WithOptimization("value-facts", facts), gcProjectionFactModule(array, value.code, use))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						in, err := Instantiate(c, InstantiateOptions{})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if _, err = in.Invoke("init"); err != nil {
							t.Fatal(err)
						}
						got, err := in.Invoke("run")
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
							t.Fatalf("run=%v,%v; want cast failure", got, err)
						}
					})
				}
			}
		}
	}
}
