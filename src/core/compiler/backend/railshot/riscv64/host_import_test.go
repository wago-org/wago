//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func hostImportModule(count, target int, types []byte, nested bool) []byte {
	call := func(index int) fixtureFunc {
		body := []byte{}
		for i := range types {
			body = append(body, 0x20)
			body = uleb(body, uint32(i))
		}
		body = append(body, 0x10)
		body = uleb(body, uint32(index))
		body = append(body, 0x0b)
		return fixtureFunc{params: types, results: types, body: body}
	}
	fs := []fixtureFunc{call(target)}
	if nested {
		fs = []fixtureFunc{call(count + 1), call(target)}
	}
	data := binaryModule(fs...)
	imports := uleb(nil, uint32(count))
	for range count {
		imports = append(imports, 1, 'm', 1, 'h', 0, 0)
	}
	data = modifySection(data, 3, 2, imports, false)
	exports := []byte{1, 1, 'a', 0}
	exports = uleb(exports, uint32(count))
	return modifySection(data, 7, 7, exports, true)
}

func hostImportBindings(count int) []ImportBinding {
	b := make([]ImportBinding, count)
	for i := range b {
		b[i] = ImportBinding{Dynamic: true, ImportIndex: uint32(i)}
	}
	return b
}

func hostImportResourceModule(count, target int, types []byte, nested bool) []byte {
	data := hostImportModule(count, target, types, nested)
	// Memory and global imports precede the function prefix on the wire. They
	// must not change the function-only dispatch ordinal or trap function index.
	imports := uleb(nil, uint32(count+2))
	imports = append(imports, 1, 'm', 1, 'b', 2, 1, 0, 1)
	imports = append(imports, 1, 'm', 1, 'g', 3, 0x7f, 0)
	for range count {
		imports = append(imports, 1, 'm', 1, 'h', 0, 0)
	}
	return modifySection(data, 2, 2, imports, true)
}

func TestHostImportRV64WholeModuleAndBindingAdmission(t *testing.T) {
	resourceModule, err := wasm.DecodeModule(hostImportResourceModule(256, 255, []byte{0x7f, 0x7e, 0x7d, 0x7c}, true))
	if err != nil {
		t.Fatal(err)
	}
	if image, err := CompileModuleWith(resourceModule, CompileOptions{ImportBindings: hostImportBindings(256), DeferCodeMapping: true}); err != nil || image == nil || len(image.Entry) != 2 {
		t.Fatal("interleaved resource imports", err)
	}
	for _, count := range []int{1, 64, 65, 256} {
		data := hostImportModule(count, count-1, []byte{0x7f, 0x7e, 0x7d, 0x7c}, true)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := AdmitProductionSource(data); err != nil {
			t.Fatal(err)
		}
		for _, sync := range []bool{false, true} {
			image, err := CompileModuleWith(m, CompileOptions{ImportBindings: hostImportBindings(count), SyncHostCalls: sync, DeferCodeMapping: true})
			if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 2 || len(image.Code) == 0 {
				t.Fatal("bounded dynamic imports rejected", count, sync, err)
			}
		}
		if image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: true}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("byte-only compiler widened without explicit bindings", err)
		}
		if image, err := Compile(data); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("isolated compiler widened", err)
		}
		for _, bad := range []ImportBinding{
			{}, {Dynamic: true, ImportIndex: 1}, {Dynamic: true, CrossInstance: true},
			{Dynamic: true, CalleeEntry: 4}, {Dynamic: true, CalleeLinMem: 8}, {Dynamic: true, EHTransfer: true},
		} {
			bindings := hostImportBindings(count)
			bindings[0] = bad
			pressure := 0
			image, err := CompileModuleWith(m, CompileOptions{ImportBindings: bindings, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
			if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("invalid binding reached image", bad, err, pressure)
			}
		}
	}
	for _, shape := range []string{"bound", "reexport", "table", "start", "hidden-op"} {
		count := 1
		if shape == "bound" {
			count = 257
		}
		data := hostImportModule(count, 0, []byte{0x7f}, false)
		switch shape {
		case "reexport":
			data = modifySection(data, 7, 7, []byte{1, 1, 'a', 0, 0}, true)
		case "table":
			data = modifySection(data, 7, 4, []byte{1, 0x70, 1, 0, 1}, false)
		case "start":
			data = hostImportModule(1, 0, nil, false)
			data = modifySection(data, 10, 8, []byte{1}, false)
		case "hidden-op":
			// Tail calls remain outside the allowlist even in unreachable code.
			data = modifySection(data, 10, 10, []byte{1, 5, 0, 0, 0x12, 0, 0x0b}, true)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(shape, err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{ImportBindings: hostImportBindings(count), DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		if image != nil || err == nil || pressure != 0 {
			t.Fatal("unsupported host module reached image", shape, err, pressure)
		}
	}
}
