//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"strings"
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
				opts := ceilingPublicOptions(0)
				opts.NativeTailCalls = route == "tail"
				if _, err = CompileModuleWith(decoded.Module, opts); err != nil {
					t.Fatal("public compile", err)
				}
			})
		}
	}
}

func TestRV64CeilingFunctionImports(t *testing.T) {
	for _, route := range []string{"scalar", "swar"} {
		t.Run(route, func(t *testing.T) {
			source := ceilingModule(1025, 257, route)
			if err := wasm.ValidateByteBackedModule(source); err != nil {
				t.Fatal(err)
			}
			d, err := wasm.DecodeModuleByteBacked(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := CompileModuleWith(d.Module, ceilingPublicOptions(257))
			if err != nil {
				t.Fatal(err)
			}
			if len(image.Entry) != 1025 {
				t.Fatal("defined directory", len(image.Entry))
			}
		})
	}
}

func TestRV64CeilingCallSites(t *testing.T) {
	source := ceilingModule(1, 0, "calls")
	if err := wasm.ValidateByteBackedModule(source); err != nil {
		t.Fatal(err)
	}
	d, err := wasm.DecodeModuleByteBacked(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CompileModuleWith(d.Module, ceilingPublicOptions(0)); err != nil {
		t.Fatal(err)
	}
}

func TestRV64CeilingAdvisoryMetadata(t *testing.T) {
	for _, route := range []string{"scalar", "swar"} {
		t.Run(route, func(t *testing.T) {
			d, err := wasm.DecodeModuleByteBacked(ceilingModule(1025, 0, route))
			if err != nil {
				t.Fatal(err)
			}
			m := d.Module
			m.Customs = make([]wasm.CustomSec, 1025)
			m.NameSec = &wasm.NameSec{FunctionNames: make(wasm.NameMap, 1025), LocalNames: make(wasm.IndirectNameMap, 1025)}
			m.BranchHints = make([]wasm.FuncBranchHints, 1025)
			m.Exports = make([]wasm.Export, 1025)
			for i := 0; i < 1025; i++ {
				m.NameSec.FunctionNames[i] = wasm.NameAssoc{Index: uint32(i), Name: "fn"}
				m.NameSec.LocalNames[i] = wasm.IndirectNameAssoc{Index: uint32(i)}
				m.BranchHints[i] = wasm.FuncBranchHints{FuncIndex: uint32(i)}
				m.Exports[i] = wasm.Export{Name: fmt.Sprint(i), Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: uint32(i)}}
			}
			if _, err = CompileModuleWith(m, ceilingPublicOptions(0)); err != nil {
				t.Fatal(err)
			}
			remaining := 16*len(m.Customs) - 1
			if err = admitSWARDecodedMetadataBudget(m, &remaining); err == nil {
				t.Fatal("empty customs bypassed byte accounting")
			}
		})
	}
}

func TestRV64CeilingPrivateAndRootedDirectories(t *testing.T) {
	source := ceilingModule(1025, 0, "scalar")
	if _, err := Compile(source); err != nil {
		t.Fatal("private scalar directory", err)
	}
	for _, force := range []bool{false, true} {
		if _, err := compileTailImportSource(ceilingModule(1025, 1, "scalar"), 0, force); err == nil || !strings.Contains(err.Error(), "metadata exceeds allocation limit 1048576") {
			t.Fatal("private tail decoder byte budget", force, err)
		}
	}
	d, err := wasm.DecodeModuleByteBacked(ceilingModule(1025, 0, "rooted"))
	if err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{production: true, publicStarts: true, gcRootedScalar: true, DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = compileDecodedModule(d.Module, scalarRegisters[:], options); err != nil {
		t.Fatal("generic rooted compile", err)
	}
	mode := emissionMode{production: true, interruptible: true, invocationContext: true, foreignTableCalls: true, gcRootedScalar: true}
	if _, err = compileSWARDecodedModuleProfile(d.Module, mode, swarModulePlain, swarStateProfile{publicStarts: true, exports: true}); err != nil {
		t.Fatal("SWAR rooted compile", err)
	}
}

func TestRV64CeilingConfiguredBudgets(t *testing.T) {
	d, err := wasm.DecodeModuleByteBacked(ceilingModule(1025, 0, "swar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []string{"code", "metadata"} {
		opts := ceilingPublicOptions(0)
		if budget == "code" {
			opts.MaxNativeCodeBytes = 1024
		} else {
			opts.MaxCompiledMetadataBytes = 1024
		}
		staged := false
		opts.MemoryPressure = func() { staged = true }
		if image, err := CompileModuleWith(d.Module, opts); image != nil || err == nil || staged {
			t.Fatal("quota bypass", budget, err, staged)
		}
	}
}

func TestRV64CeilingCallRecordBudget(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	state := moduleState{production: true, functions: []functionSpec{{signature: ft}}}
	for _, count := range []int{1025, maxFunctionCallSites + 1} {
		// Dead calls still consume admission work/record budget without producing
		// executable recursive calls. The existing instruction-work budget may bind
		// before the larger call-record extent; neither budget is widened.
		body := []byte{0}
		for i := 0; i < count; i++ {
			body = append(body, 0x10, 0)
		}
		body = append(body, 0x0b)
		fn := &wasm.Func{BodyBytes: body}
		_, integerErr := walkInteger(fn, ft, &state, nil)
		_, swarErr := walkSWARPairsWithCalls(fn, ft, false, nil, []*wasm.CompType{ft}, nil)
		if (integerErr == nil) != (count == 1025) || (swarErr == nil) != (count == 1025) {
			t.Fatal("call record budget", count, integerErr, swarErr)
		}
	}
}
