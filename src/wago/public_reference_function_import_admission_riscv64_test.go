//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportCopy(c *Compiled) *Compiled {
	v := rv64ReferenceStateCopy(c)
	v.Imports = slices.Clone(v.Imports)
	v.importFuncSigs = slices.Clone(v.importFuncSigs)
	for i := range v.importFuncSigs {
		v.importFuncSigs[i].Params = slices.Clone(v.importFuncSigs[i].Params)
		v.importFuncSigs[i].Results = slices.Clone(v.importFuncSigs[i].Results)
	}
	v.FuncTypeID = slices.Clone(v.FuncTypeID)
	v.Exports = maps.Clone(v.Exports)
	return v
}

func TestRV64PublicReferenceFunctionImportsRetainedAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-imports-admission", wasmtest.ClosedReferenceImports(vector, true, true), false)
		if err := rv64ReferenceImportCopy(c).validate(); err != nil {
			t.Fatal("valid numeric imports", err)
		}
		mutations := map[string]func(*Compiled){
			"negative-imports":     func(v *Compiled) { v.NumImports = -1 },
			"count-over":           func(v *Compiled) { v.NumImports = railshot.MaxHostFunctionImports + 1 },
			"missing-name":         func(v *Compiled) { v.Imports = v.Imports[:2] },
			"missing-signature":    func(v *Compiled) { v.importFuncSigs = v.importFuncSigs[:2] },
			"missing-dispatch":     func(v *Compiled) { v.dynamicImports = false },
			"dynamic-escape":       func(v *Compiled) { v.dynamicFuncrefEscape = true },
			"missing-exact-type":   func(v *Compiled) { v.importFuncSigs[2].HasTypeIndex = false },
			"late-invalid-type":    func(v *Compiled) { v.importFuncSigs[2].TypeIndex = 999 },
			"late-wrong-type":      func(v *Compiled) { v.importFuncSigs[2].TypeIndex = 1 },
			"late-reference-type":  func(v *Compiled) { v.importFuncSigs[2] = v.Funcs[2] },
			"late-wrong-carrier":   func(v *Compiled) { v.importFuncSigs[2].Params[0] = ValI64 },
			"missing-imported-key": func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[1:] },
			"late-imported-key":    func(v *Compiled) { v.FuncTypeID[2] = 999 },
			"shifted-defined-key":  func(v *Compiled) { v.FuncTypeID[7] = v.FuncTypeID[3] },
			"last-export-over":     func(v *Compiled) { v.Exports["hidden"] = v.NumImports + len(v.Funcs) },
			"bad-name-separator":   func(v *Compiled) { v.Imports[2] = "env/owned" },
			"bad-name-end":         func(v *Compiled) { v.validateMemo = &validateMemo{importModuleEnds: []uint64{4, 4, 99}} },
			"missing-name-end":     func(v *Compiled) { v.validateMemo = &validateMemo{importModuleEnds: []uint64{4, 4}} },
		}
		for name, edit := range mutations {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceImportCopy(c)
				edit(v)
				if err := v.validate(); err == nil {
					t.Fatal("malformed imported reference metadata accepted")
				}
			})
		}
		for _, name := range []string{"missing-dispatch", "dynamic-escape", "late-invalid-type", "late-wrong-type", "late-reference-type", "late-wrong-carrier", "missing-imported-key", "late-imported-key", "shifted-defined-key", "last-export-over"} {
			t.Run(fmt.Sprintf("vector%t-codec-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceImportCopy(c)
				mutations[name](v)
				blob, err := marshalCompiled(v)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed imported reference artifact loaded")
				}
			})
		}
		public := rv64ReferenceImportCopy(c)
		public.Exports["reference"] = 5
		if err := public.validate(); err != nil {
			t.Fatal("ephemeral imported-profile reference export", err)
		}
		blob, err := marshalCompiled(public)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal("ephemeral imported-profile reference artifact", err)
		}
		loaded.Close()
		// Charge all imported names, declared carriers and owned state together.
		v := rv64ReferenceImportCopy(c)
		name := "env." + strings.Repeat("n", railshot.MaxModuleBytes)
		lo, hi := 4, len(name)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			v.Imports[2] = name[:mid]
			if rv64ValidateReferenceFunctions(v) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		v.Imports[2] = name[:lo]
		if lo == 4 || v.validate() != nil {
			t.Fatal("exact retained import name bound")
		}
		v.Imports[2] = name[:lo+1]
		if err := rv64ValidateReferenceFunctions(v); err == nil {
			t.Fatal("retained name plus one")
		}
		v.Imports[2] = name[:lo]
		v.Imports[0] += "x"
		if err := rv64ValidateReferenceFunctions(v); err == nil {
			t.Fatal("retained names not aggregated")
		}
	}
}

func rv64ReferenceUnusedNumericImport(wide bool) []byte {
	params := []wasm.ValType{wasm.V128}
	if wide {
		params = make([]wasm.ValType, 65)
		for i := range params {
			params[i] = wasm.I32
		}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, nil), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("unused")...), 0, 0))), wasmtest.Section(3, []byte{2, 1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x10, 2, 0x1a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}

func TestRV64PublicReferenceFunctionImportsRejectHostShapes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-wide%t", artifact, wide), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-imports-unused-wide%t", wide), rv64ReferenceUnusedNumericImport(wide), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				imports := NewImports().Function("env", "unused", HostCallFunc(func(HostCall) { t.Fatal("unsupported unused host called") }))
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if in != nil {
					in.Close()
				}
				if in != nil || !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("unsupported unused host published", err)
				}
				if cc := c.loadCodeCache(); cc != nil && (cc.sealed || cc.refs != 0) {
					t.Fatal("unsupported unused host acquired executable mapping")
				}
			})
		}
	}
}
