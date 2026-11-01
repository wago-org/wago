//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func ceilingModule(n, imports int, route string) []byte {
	source := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	types := []byte{1, 0x60, 0, 0}
	if route == "rooted" {
		types = []byte{2, 0x60, 0, 0, 0x5f, 0}
	}
	source = append(source, typeCapSection(1, types)...)
	if imports != 0 {
		payload := typeCapU32(imports)
		for i := 0; i < imports; i++ {
			payload = append(payload, 1, 'm', 1, 'f', 0, 0)
		}
		source = append(source, typeCapSection(2, payload)...)
	}
	funcs, code := typeCapU32(n), typeCapU32(n)
	for i := 0; i < n; i++ {
		funcs = append(funcs, 0)
		body := []byte{0}
		if i == n-1 {
			switch route {
			case "swar":
				body = append(body, 0xfd, 12)
				body = append(body, make([]byte, 16)...)
				body = append(body, 0x1a)
			case "table":
				body = append(body, 0x41, 0, 0x11, 0, 0)
			case "tail":
				body = append(body, 0x12, 0)
			case "calls":
				for j := 0; j < 1025; j++ {
					body = append(body, 0x10, 0)
				}
			}
		}
		body = append(body, 0x0b)
		code = append(code, typeCapU32(len(body))...)
		code = append(code, body...)
	}
	source = append(source, typeCapSection(3, funcs)...)
	if route == "table" {
		source = append(source, typeCapSection(4, []byte{1, 0x70, 0, 1})...)
	}
	return append(source, typeCapSection(10, code)...)
}

func ceilingPublicOptions(imports int) CompileOptions {
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if imports != 0 {
		opts.SyncHostCalls = true
		opts.SyncHostSlots = 512
		opts.ImportBindings = make([]ImportBinding, imports)
		for i := range opts.ImportBindings {
			opts.ImportBindings[i] = ImportBinding{Dynamic: true, ImportIndex: uint32(i)}
		}
	}
	return opts
}

func TestRV64CeilingModuleRoutes(t *testing.T) {
	for _, route := range []string{"scalar", "swar", "table", "rooted", "tail"} {
		for _, n := range []int{1024, 1025, 4096} {
			t.Run(fmt.Sprintf("%s/%d", route, n), func(t *testing.T) {
				source := ceilingModule(n, 0, route)
				if err := wasm.ValidateByteBackedModule(source); err != nil {
					t.Fatal("validation", err)
				}
				decoded, err := wasm.DecodeModuleByteBacked(source)
				if err != nil {
					t.Fatal(err)
				}
				if route == "rooted" && !AdmitGCRootedScalarSource(source) {
					t.Fatal("rooted source rejected")
				}
				if _, err = CompileModuleWith(decoded.Module, ceilingPublicOptions(0)); err != nil {
					t.Fatal("public compile", err)
				}
			})
		}
	}
}
