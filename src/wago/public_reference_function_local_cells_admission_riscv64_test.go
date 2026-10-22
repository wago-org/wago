//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionLocalCellsAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-local-cells-admission", wasmtest.ClosedReferenceLocalCells(vector, true, true, 1), false)
		refIndex := c.Globals[2].ValueTypeIndex
		for name, change := range map[string]func(*Compiled){
			"immutable-private-target": func(v *Compiled) {
				v.Globals[2].Mutable = false
				v.Globals[2].HasInitFunc = true
				v.Globals[2].InitFunc = 4
			},
			"export":                   func(v *Compiled) { v.GlobalExports["cell"] = 2 },
			"private-initializer":      func(v *Compiled) { v.Globals[3].InitFunc = 4 },
			"late-private-initializer": func(v *Compiled) { v.Globals[3].InitFunc = 9 },
			"missing-initializer":      func(v *Compiled) { v.Globals[3].InitFunc = 11 },
			"inactive-function-index":  func(v *Compiled) { v.Globals[2].InitFunc = 1 },
			"inactive-global-index":    func(v *Compiled) { v.Globals[2].InitGlobal = 1 },
			"copy":                     func(v *Compiled) { v.Globals[2].HasInitGlobal = true; v.Globals[2].InitGlobal = 3 },
			"expression":               func(v *Compiled) { v.Globals[2].InitExpr = []byte{0xd0, 0x70, 0x0b} },
			"raw-bits":                 func(v *Compiled) { v.Globals[2].Bits = 0xdeadbeef00000001 },
			"inactive-vector":          func(v *Compiled) { v.Globals[2].V128[0] = 1 },
			"nonnullable":              func(v *Compiled) { v.ValueTypes[refIndex].Ref.Nullable = false },
			"indexed":                  func(v *Compiled) { v.ValueTypes[refIndex].Ref.Heap.Defined = true },
			"extern":                   func(v *Compiled) { v.ValueTypes[refIndex].Ref.Heap.Abstract = AbstractHeapExtern },
			"bottom":                   func(v *Compiled) { v.ValueTypes[refIndex].Ref.Heap.Abstract = AbstractHeapNoFunc },
			"duplicate-pool":           func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, v.ValueTypes[refIndex]) },
			"missing-binding":          func(v *Compiled) { v.Globals[2].HasValueType = false },
			"wrong-binding":            func(v *Compiled) { v.Globals[2].ValueTypeIndex = v.Globals[0].ValueTypeIndex },
			"reference-import": func(v *Compiled) {
				v.Globals[0].Type = ValFuncRef
				v.Globals[0].ValueTypeIndex = refIndex
				v.GlobalImports[0].Type = ValFuncRef
				v.GlobalImports[0].ValueTypeIndex = refIndex
			},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceStartCopy(c)
				change(v)
				if name == "inactive-function-index" || name == "inactive-global-index" {
					rv64RequireTableArtifactMetadata(t, v)
					return
				}
				if err := v.validate(); err == nil {
					t.Fatal("malformed private reference cell admitted")
				}
				// Writers erase inactive selector fields; validate them separately.
				if strings.HasPrefix(name, "inactive-") {
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
					t.Fatal("malformed private reference cell artifact loaded")
				}
			})
		}
		owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
		defer rv64HostCertificateCloseRuntime(t, owners)
		provider, _, _, im, _ := rv64ReferenceStartProvider(t, owners, false, vector, true, false)
		calls := 0
		im.Function("env.calls", "unused.fn", func(v int32) int32 { return v }).Function("env.calls", "finish.fn", func(v int32) int32 { return v }).Function("env.calls", "start.fn", func() { calls++ })
		roots := provider.referenceLifetime().snapshot().ResourceRoots
		rt := NewRuntime()
		mod, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
		mod.Close()
		rt.Close()
		if waitErr := rt.WaitClosed(context.Background()); waitErr != nil {
			t.Fatal(waitErr)
		}
		if in == nil || err != nil || calls != 1 || c.loadCodeCache().refs != 0 || provider.referenceLifetime().snapshot().ResourceRoots != roots {
			t.Fatal("public reference local cells or root release", err, calls)
		}
	}
}

func TestRV64PublicReferenceFunctionLocalCellsAggregateBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "reference-local-cells-budget", wasmtest.ClosedReferenceLocalCells(vector, true, true, 1), false)
			v := rv64ReferenceStartCopy(c)
			padding := make([]byte, railshot.MaxModuleBytes)
			v.PassiveData = append(slices.Clone(v.PassiveData), PassiveDataInit{})
			last := len(v.PassiveData) - 1
			lo, hi := 0, len(padding)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				v.PassiveData[last].Bytes = padding[:mid]
				if rv64ValidateReferenceFunctions(v) == nil {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			v.PassiveData[last].Bytes = padding[:lo]
			if lo == 0 || rv64ValidateReferenceFunctions(v) != nil {
				t.Fatal("exact cell-bearing retained budget")
			}
			v.Globals = append(v.Globals, v.Globals[2])
			if err := rv64ValidateReferenceFunctions(v); err == nil {
				t.Fatal("extra reference cell escaped retained aggregate budget")
			}
			v.Globals = v.Globals[:len(v.Globals)-1]
			if err := rv64ValidateReferenceFunctions(v); err != nil {
				t.Fatal("retained cell budget not restored", err)
			}
			v.PassiveData[last].Bytes = padding[:lo+1]
			if err := rv64ValidateReferenceFunctions(v); err == nil {
				t.Fatal("retained cell budget+1 admitted")
			}
		})
	}
}
