//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceResourceImportCopy(c *Compiled) *Compiled {
	v := rv64ReferenceImportCopy(c)
	v.GlobalImports = slices.Clone(v.GlobalImports)
	v.GlobalExports = maps.Clone(v.GlobalExports)
	v.memoryDir.exports = maps.Clone(v.memoryDir.exports)
	v.validateMemo.importModuleEnds = slices.Clone(c.loadValidateMemo().importModuleEnds)
	return v
}

func TestRV64PublicReferenceFunctionResourceImportsRetainedAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-resource-import-admission", wasmtest.ClosedReferenceResourceImports(vector, true, true), false)
		if err := rv64ReferenceResourceImportCopy(c).validate(); err != nil {
			t.Fatal("valid numeric resource imports", err)
		}
		mutations := map[string]func(*Compiled){
			"missing-exact-sidecar":              func(v *Compiled) { v.validateMemo = nil },
			"empty-exact-sidecar":                func(v *Compiled) { v.validateMemo.importModuleEnds = nil },
			"short-exact-sidecar":                func(v *Compiled) { v.validateMemo.importModuleEnds = v.validateMemo.importModuleEnds[:5] },
			"extra-exact-sidecar":                func(v *Compiled) { v.validateMemo.importModuleEnds = append(v.validateMemo.importModuleEnds, 1) },
			"legacy-function-sidecar":            func(v *Compiled) { v.validateMemo.importModuleEnds[2] = 0 },
			"legacy-memory-sidecar":              func(v *Compiled) { v.validateMemo.importModuleEnds[5] = 0 },
			"wrong-function-separator":           func(v *Compiled) { v.validateMemo.importModuleEnds[2] = 999 },
			"wrong-memory-separator":             func(v *Compiled) { v.validateMemo.importModuleEnds[5] = 999 },
			"global-import-count":                func(v *Compiled) { v.GlobalImports = make([]GlobalImportDef, len(v.Globals)+1) },
			"late-global-missing-value-type":     func(v *Compiled) { v.GlobalImports[3].HasValueType = false },
			"late-global-wrong-value-type":       func(v *Compiled) { v.GlobalImports[3].ValueTypeIndex = v.GlobalImports[0].ValueTypeIndex },
			"late-global-invalid-value-type":     func(v *Compiled) { v.GlobalImports[3].ValueTypeIndex = 999 },
			"late-global-wrong-kind":             func(v *Compiled) { v.GlobalImports[3].Type = ValI32 },
			"late-global-reference":              func(v *Compiled) { v.GlobalImports[3].Type = ValFuncRef },
			"late-global-mutable":                func(v *Compiled) { v.GlobalImports[3].Mutable = true },
			"late-placeholder-kind":              func(v *Compiled) { v.Globals[3].Type = ValI32 },
			"late-placeholder-mutable":           func(v *Compiled) { v.Globals[3].Mutable = true },
			"late-placeholder-type":              func(v *Compiled) { v.Globals[3].ValueTypeIndex = 999 },
			"late-placeholder-init-bits":         func(v *Compiled) { v.Globals[3].Bits = 1 },
			"late-placeholder-init-vector":       func(v *Compiled) { v.Globals[3].V128[15] = 1 },
			"late-placeholder-init-function":     func(v *Compiled) { v.Globals[3].HasInitFunc = true },
			"late-placeholder-inactive-function": func(v *Compiled) { v.Globals[3].InitFunc = 1 },
			"late-placeholder-init-global":       func(v *Compiled) { v.Globals[3].HasInitGlobal = true },
			"late-placeholder-inactive-global":   func(v *Compiled) { v.Globals[3].InitGlobal = 1 },
			"late-placeholder-init-expression":   func(v *Compiled) { v.Globals[3].InitExpr = []byte{0x42, 0, 0x0b} },
			"missing-primary-import":             func(v *Compiled) { v.memoryImport = "" },
			"wrong-primary-import":               func(v *Compiled) { v.memoryImport = "env.resources.memory.1" },
			"primary-max-disagrees":              func(v *Compiled) { v.MemHasMax = false },
			"late-import-after-owned": func(v *Compiled) {
				v.memoryDir.defs[3].ImportKey = v.memoryDir.defs[1].ImportKey
				v.memoryDir.defs[1].ImportKey = ""
			},
			"late-memory-shared":           func(v *Compiled) { v.memoryDir.defs[2].Shared = true },
			"late-memory-inactive-max":     func(v *Compiled) { v.memoryDir.defs[2].HasMax = false },
			"late-memory-limits":           func(v *Compiled) { v.memoryDir.defs[2].Min = 4 },
			"memory-count":                 func(v *Compiled) { v.memoryDir.defs = make([]memoryDef, railshot.MaxProductionMemories+1) },
			"global-count":                 func(v *Compiled) { v.Globals = make([]GlobalDef, 257) },
			"imported-global-data-width":   func(v *Compiled) { v.Data[0].Offset.Expr = []byte{0x23, 0, 0x0b} },
			"data-full-namespace":          func(v *Compiled) { v.Data[0].MemoryIndex = 4 },
			"memory-export-full-namespace": func(v *Compiled) { v.memoryDir.exports["late"] = 4 },
			"global-export-full-namespace": func(v *Compiled) { v.GlobalExports["late"] = 8 },
		}
		for name, edit := range mutations {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceResourceImportCopy(c)
				edit(v)
				switch name {
				case "missing-primary-import", "wrong-primary-import", "primary-max-disagrees", "late-placeholder-init-vector", "late-placeholder-inactive-function", "late-placeholder-inactive-global":
					rv64RequireTableArtifactMetadata(t, v)
					return
				}
				if err := v.validate(); err == nil {
					t.Fatal("malformed resource imports accepted")
				}
				// These fields are runtime caches or inactive initializer payloads,
				// absent from the wire encoding. The codec canonicalizes them.
				switch name {
				case "missing-primary-import", "wrong-primary-import", "primary-max-disagrees", "late-placeholder-init-vector", "late-placeholder-inactive-function", "late-placeholder-inactive-global":
					return
				}
				blob, err := marshalCompiled(v)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed resource import artifact loaded")
				}
			})
		}
		// Exercise aggregate state and all three kinds of import names at exactly
		// the retained budget, rather than mirroring the validator's formula.
		v := rv64ReferenceResourceImportCopy(c)
		buffer := strings.Repeat("n", railshot.MaxModuleBytes)
		lo, hi := 0, len(buffer)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			v.GlobalImports[3].Name = buffer[:mid]
			if rv64ValidateReferenceFunctions(v) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		v.GlobalImports[3].Name = buffer[:lo]
		if lo == 0 || rv64ValidateReferenceFunctions(v) != nil {
			t.Fatal("exact retained resource name budget")
		}
		v.GlobalImports[3].Name = buffer[:lo+1]
		if rv64ValidateReferenceFunctions(v) == nil {
			t.Fatal("resource name plus one")
		}
		v.GlobalImports[3].Name = buffer[:lo]
		v.memoryDir.defs[2].ImportKey += "x"
		if rv64ValidateReferenceFunctions(v) == nil {
			t.Fatal("memory/global names not aggregated")
		}
		v.memoryDir.defs[2].ImportKey = v.memoryDir.defs[2].ImportKey[:len(v.memoryDir.defs[2].ImportKey)-1]
		v.Imports[2] += "x"
		if rv64ValidateReferenceFunctions(v) == nil {
			t.Fatal("function/global names not aggregated")
		}
		v.Imports[2] = v.Imports[2][:len(v.Imports[2])-1]
		v.PassiveData[2].Bytes = append(slices.Clone(v.PassiveData[2].Bytes), 0)
		if rv64ValidateReferenceFunctions(v) == nil {
			t.Fatal("resource name/state not aggregated")
		}
	}
}
