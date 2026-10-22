//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportTailCopy(c *Compiled) *Compiled {
	v := cloneCompiledMetadata(c.executionView())
	if cache := c.loadCodeCache(); cache != nil {
		// Each mutant owns its staging cache. Do not copy a mutex or share an
		// executable mapping with the unchanged fixture.
		v.codeCache = &compiledCodeCache{
			gcMetadataFlags:        cache.gcMetadataFlags,
			gcTypeSubtypingProduct: cache.gcTypeSubtypingProduct,
			gcStructProduct:        cache.gcStructProduct,
			gcArrayProduct:         cache.gcArrayProduct,
			gcI31Product:           cache.gcI31Product,
			flags:                  cache.flags,
			stagedFeatures:         cache.stagedFeatures,
		}
	}
	indexes := *c.loadCompileIndexes()
	if module := rv64NativeTailModule(c); module != nil {
		indexes.railshotTail.module = &encoder.NativeTailModule{Plans: slices.Clone(module.Plans)}
	}
	v.validateMemo = &validateMemo{compileIndexes: &indexes, importModuleEnds: slices.Clone(c.loadValidateMemo().importModuleEnds)}
	return v
}

func TestRV64PublicReferenceFunctionImportedTailsRetainedAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "import-tail-admission", wasmtest.ClosedReferenceImportTails(vector, true), false)
		if err := rv64ReferenceImportTailCopy(c).validate(); err != nil {
			t.Fatal("valid private imported tails", err)
		}
		for name, change := range map[string]func(*Compiled){
			"indirect":             func(v *Compiled) { rv64NativeTailModule(v).Plans[0].Kind = encoder.NativeTailIndirect },
			"reference":            func(v *Compiled) { rv64NativeTailModule(v).Plans[0].Kind = encoder.NativeTailReference },
			"unknown-kind":         func(v *Compiled) { rv64NativeTailModule(v).Plans[0].Kind = 3 },
			"inactive-type":        func(v *Compiled) { rv64NativeTailModule(v).Plans[0].TypeIndex = 1 },
			"inactive-reference":   func(v *Compiled) { rv64NativeTailModule(v).Plans[0].ReferenceBase = 48 },
			"defined-target":       func(v *Compiled) { rv64NativeTailModule(v).Plans[0].ImportIndex = 3 },
			"import-source":        func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FunctionIndex = 2 },
			"missing-source":       func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FunctionIndex = 6 },
			"numeric-root-results": func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FunctionIndex = 3 },
			"short-frame":          func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FrameBytes = 32 },
			"huge-frame":           func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FrameBytes = railshot.MaxFrameBytes + 16 },
			"unaligned-frame":      func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FrameBytes++ },
			"header-home":          func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceBase = 40 },
			"unaligned-home":       func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceBase++ },
			"out-of-frame-home":    func(v *Compiled) { p := &rv64NativeTailModule(v).Plans[0]; p.SourceBase = p.FrameBytes },
			"invalid-stride":       func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceStride = 32 },
			"plan-bound":           func(v *Compiled) { rv64NativeTailModule(v).Plans = make([]encoder.NativeTailPlan, 4097) },
			"reference-import":     func(v *Compiled) { v.importFuncSigs[0] = v.Funcs[2] },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceImportTailCopy(c)
				change(v)
				if err := v.validate(); err == nil {
					t.Fatal("malformed private imported tail admitted")
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
					t.Fatal("malformed private imported-tail artifact loaded")
				}
			})
		}
		{
			publicSource := rv64ReferenceAddSourceExport(t, wasmtest.ClosedReferenceImportTails(vector, true), wasmtest.ExportEntry("reference", 0, 5))
			public := rv64NativeTailCompile(t, "reference-import-public-export", publicSource, false)
			if err := public.validate(); err != nil {
				t.Fatal("ephemeral imported-tail reference export", err)
			}
			blob, err := marshalCompiled(public)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal("ephemeral imported-tail reference artifact", err)
			}
			loaded.Close()
		}
		// Persisted plans must keep the source frame binding.
		v := rv64ReferenceImportTailCopy(c)
		rv64NativeTailModule(v).Plans[0].FrameBytes += 16
		if err := v.validate(); err == nil {
			t.Fatal("artifact changed the compiled tail frame")
		}
		if blob, err := marshalCompiled(v); err == nil {
			loaded, loadErr := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if loadErr == nil {
				t.Fatal("mismatched tail frame artifact loaded")
			}
		}
	}
}
