//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceStateCopy(c *Compiled) *Compiled {
	copy := *c.executionView()
	copy.Types = cloneDefinedTypeDescriptors(copy.Types)
	copy.ValueTypes = slices.Clone(copy.ValueTypes)
	copy.Globals = slices.Clone(copy.Globals)
	for i := range copy.Globals {
		copy.Globals[i].InitExpr = slices.Clone(copy.Globals[i].InitExpr)
	}
	dir := *copy.memoryDir
	dir.defs = slices.Clone(dir.defs)
	copy.memoryDir = &dir
	copy.compactData = nil
	copy.Data = make([]DataInit, c.activeDataCount())
	for i := range copy.Data {
		copy.Data[i] = c.activeDataAt(i)
		copy.Data[i].Offset.Expr = slices.Clone(copy.Data[i].Offset.Expr)
		copy.Data[i].Bytes = slices.Clone(copy.Data[i].Bytes)
	}
	copy.PassiveData = slices.Clone(copy.PassiveData)
	return &copy
}

func TestRV64PublicReferenceFunctionStateRetainedAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-state-admission", wasmtest.ClosedReferenceState(vector, false, true), false)
		if err := rv64ReferenceStateCopy(c).validate(); err != nil {
			t.Fatal("valid retained state", err)
		}
		mutations := map[string]func(*Compiled){
			"pool-unused":                  func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, ValueTypeDescriptor{Kind: ValueTypeI64}) },
			"pool-duplicate":               func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, v.ValueTypes[0]) },
			"pool-reference-payload":       func(v *Compiled) { v.ValueTypes[0].Ref.Nullable = true },
			"pool-reference-kind":          func(v *Compiled) { v.ValueTypes[0].Kind = ValueTypeReference },
			"global-nonexact":              func(v *Compiled) { v.Globals[0].HasValueType = false },
			"global-bad-binding":           func(v *Compiled) { v.Globals[0].ValueTypeIndex = 99 },
			"global-wrong-abi":             func(v *Compiled) { v.Globals[0].Type = ValI64 },
			"global-count":                 func(v *Compiled) { v.Globals = make([]GlobalDef, 257) },
			"global-reference-initializer": func(v *Compiled) { v.Globals[0].HasInitFunc = true },
			"global-inactive-function":     func(v *Compiled) { v.Globals[0].InitFunc = 1 },
			"global-inactive-index":        func(v *Compiled) { v.Globals[0].InitGlobal = 1 },
			"global-inactive-vector":       func(v *Compiled) { v.Globals[0].V128[0] = 1 },
			"global-i32-upper":             func(v *Compiled) { v.Globals[0].Bits |= 1 << 32 },
			"global-expression-bits":       func(v *Compiled) { v.Globals[1].Bits = 2 },
			"global-expression-vector":     func(v *Compiled) { v.Globals[1].V128[0] = 1 },
			"global-expression-index":      func(v *Compiled) { v.Globals[1].HasInitGlobal = true },
			"global-expression-large":      func(v *Compiled) { v.Globals[1].InitExpr = make([]byte, railshot.MaxIntegerInitializerBytes+1) },
			"global-expression-wrong-type": func(v *Compiled) { v.Globals[1].InitExpr = []byte{0x42, 0, 0x0b} },
			"memory-count":                 func(v *Compiled) { v.memoryDir.defs = make([]memoryDef, railshot.MaxProductionMemories+1) },
			"memory-shared":                func(v *Compiled) { v.memoryDir.defs[1].Shared = true },
			"memory-inactive-max":          func(v *Compiled) { v.memoryDir.defs[1].HasMax = false },
			"memory-late-import":           func(v *Compiled) { v.memoryDir.defs[1].ImportKey = "env.memory" },
			"memory-advisory-exports":      func(v *Compiled) { v.memoryDir.exactExports = false },
			"memory-export":                func(v *Compiled) { v.memoryDir.exports = map[string]int{"memory": 2} },
			"memory-primary-cache":         func(v *Compiled) { v.MemMinPages++ },
			"data-count":                   func(v *Compiled) { v.Data = make([]DataInit, 257) },
			"passive-count":                func(v *Compiled) { v.PassiveData = make([]PassiveDataInit, 257) },
			"data-index":                   func(v *Compiled) { v.Data[0].MemoryIndex = 2 },
			"offset-inactive-global":       func(v *Compiled) { v.Data[0].Offset.Global = 1 },
			"offset-expression-base":       func(v *Compiled) { v.Data[0].Offset.Base = 32 },
			"offset-expression-global":     func(v *Compiled) { v.Data[0].Offset.HasGlobal = true },
			"offset-expression-large":      func(v *Compiled) { v.Data[0].Offset.Expr = make([]byte, railshot.MaxIntegerInitializerBytes+1) },
			"offset-i64-missing":           func(v *Compiled) { v.Data[0].Offset = OffsetInit{Base: 32} },
			"offset-i64-wrong-type":        func(v *Compiled) { v.Data[0].Offset.Expr = []byte{0x41, 32, 0x0b} },
		}
		if vector {
			mutations["global-vector-inactive-bits"] = func(v *Compiled) { v.Globals[2].Bits = 1 }
		}
		for name, mutate := range mutations {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				copy := rv64ReferenceStateCopy(c)
				mutate(copy)
				if err := copy.validate(); err == nil {
					t.Fatal("malformed owned state accepted")
				}
			})
		}
		// The codec canonicalizes inactive payloads and exact-export flags. These
		// persisted mutations survive serialization and must fail the loader too.
		// An out-of-order import is tested directly: the internal writer requires
		// well-formed import directories before it can encode module-name ends.
		for _, name := range []string{"pool-unused", "pool-duplicate", "global-bad-binding", "global-wrong-abi", "global-expression-large", "global-expression-wrong-type", "memory-shared", "memory-export", "data-index", "offset-expression-large", "offset-i64-missing", "offset-i64-wrong-type"} {
			t.Run(fmt.Sprintf("vector%t-codec-%s", vector, name), func(t *testing.T) {
				copy := rv64ReferenceStateCopy(c)
				mutations[name](copy)
				blob, err := marshalCompiled(copy)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed retained state loaded")
				}
			})
		}
		copy := rv64ReferenceStateCopy(c)
		copy.memoryDir.defs[1].HasMax, copy.memoryDir.defs[1].Max = false, 0
		if err := copy.validate(); err != nil {
			t.Fatal("canonical no-maximum owned memory", err)
		}
		copy = rv64ReferenceStateCopy(c)
		buffer := make([]byte, railshot.MaxModuleBytes)
		lo, hi := 0, len(buffer)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			copy.PassiveData[2].Bytes = buffer[:mid]
			if rv64ValidateReferenceFunctions(copy) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		copy.PassiveData[2].Bytes = buffer[:lo]
		if lo == 0 || copy.validate() != nil {
			t.Fatal("exact combined retained state/type budget")
		}
		copy.PassiveData[2].Bytes = buffer[:lo+1]
		if err := rv64ValidateReferenceFunctions(copy); err == nil || !strings.Contains(err.Error(), "aggregate declared carrier byte bound") {
			t.Fatal("state and types were not charged together", err)
		}
		copy.PassiveData[2].Bytes = buffer[:lo]
		copy.Globals[1].InitExpr = append(copy.Globals[1].InitExpr, 0)
		if err := rv64ValidateReferenceFunctions(copy); err == nil || !strings.Contains(err.Error(), "aggregate declared carrier byte bound") {
			t.Fatal("initializer and types were not charged together", err)
		}
	}
}

func TestRV64PublicReferenceFunctionStateInitializers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, offset := range []uint64{0, 32, 1 << 32, 1<<32 | 32, 1 << 63, ^uint64(0)} {
				key := fmt.Sprintf("reference-state-init-v%t-offset%x", vector, offset)
				t.Run(fmt.Sprintf("artifact%t-%s", artifact, key), func(t *testing.T) {
					c := rv64NativeTailCompile(t, key, wasmtest.ClosedReferenceStateOffset(vector, offset), artifact)
					wantExpr := append(append([]byte{0x42}, wasmtest.SLEB64(int64(offset))...), 0x0b)
					data := c.activeDataAt(0)
					if data.Offset.Base != 0 || !bytes.Equal(data.Offset.Expr, wantExpr) {
						t.Fatal("full initializer address was not retained")
					}
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rt.Close()
					mod, err := rt.Module(c)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					in, err := rt.Instantiate(context.Background(), mod)
					if offset >= 65536 {
						if in != nil {
							in.Close()
						}
						if in != nil || err == nil || !strings.Contains(err.Error(), "out of bounds") {
							t.Fatal("high initializer address truncated", offset, err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if !bytes.Equal(in.memoryDir.memories[0].UnsafeBytes()[offset:offset+2], []byte{0x21, 0x41}) || readGlobalObject(in.globalCells[1], ValI32) != 11 {
						t.Fatal("literal/extended initializer execution")
					}
				})
			}
		}
	}
}
