//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func TestRV64ImportedInitializerArtifactBoundaries(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedInitializerModule(1, []byte{42}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"defined-global", func(v *Compiled) { v.Globals[5].InitGlobal = 4 }},
		{"negative-global", func(v *Compiled) { v.Globals[4].InitGlobal = -1 }},
		{"unavailable-global", func(v *Compiled) { v.Globals[4].InitGlobal = 8 }},
		{"same-width-f32", func(v *Compiled) { v.Globals[4].InitGlobal = 2 }},
		{"same-width-f64", func(v *Compiled) { v.Globals[5].InitGlobal = 3 }},
		{"mutable-source", func(v *Compiled) { v.GlobalImports[0].Mutable = true; v.Globals[0].Mutable = true }},
		{"imported-destination", func(v *Compiled) { v.Globals[0].HasInitGlobal = true; v.Globals[0].InitGlobal = 0 }},
		{"mixed-global-expression", func(v *Compiled) { v.Globals[4].InitExpr = []byte{0x23, 0, 0x0b} }},
		{"mixed-function-expression", func(v *Compiled) { v.Globals[4].HasInitFunc = true }},
		{"defined-offset", func(v *Compiled) { v.Data[0].Offset.Global = 4 }},
		{"negative-offset", func(v *Compiled) { v.Data[0].Offset.Global = -1 }},
		{"unavailable-offset", func(v *Compiled) { v.Data[0].Offset.Global = 8 }},
		{"wrong-width-offset", func(v *Compiled) { v.Data[0].Offset.Global = 1 }},
		{"same-width-f32-offset", func(v *Compiled) { v.Data[0].Offset.Global = 2 }},
		{"mixed-offset-expression", func(v *Compiled) { v.Data[0].Offset.Expr = []byte{0x23, 0, 0x0b} }},
		{"no-memory-global-offset", func(v *Compiled) {
			v.MemMinPages = 0
			v.MemMaxPages = 0
			v.MemHasMax = false
			v.HasMemory = false
			v.memoryDir = nil
			v.memoryImport = ""
		}},
		{"no-memory-literal-offset", func(v *Compiled) {
			v.MemMinPages = 0
			v.MemMaxPages = 0
			v.MemHasMax = false
			v.HasMemory = false
			v.memoryDir = nil
			v.memoryImport = ""
			v.Data[0].Offset = OffsetInit{Base: 7}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := *base
			v.Globals = slices.Clone(base.Globals)
			v.GlobalImports = slices.Clone(base.GlobalImports)
			v.Data = slices.Clone(base.Data)
			tc.mutate(&v)
			if err := railshotValidateCompiled(&v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unsupported initializer metadata escaped RV64 gate", err)
			}
		})
	}
}
