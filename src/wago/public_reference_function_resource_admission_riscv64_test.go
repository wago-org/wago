//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceResourceCopy(c *Compiled) *Compiled {
	copy := rv64ReferenceStateCopy(c)
	copy.Exports = maps.Clone(copy.Exports)
	copy.GlobalExports = maps.Clone(copy.GlobalExports)
	copy.memoryDir.exports = maps.Clone(copy.memoryDir.exports)
	return copy
}

func TestRV64PublicReferenceFunctionResourceAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-resource-admission", wasmtest.ClosedReferenceResources(vector, false, true), false)
		if err := rv64ReferenceResourceCopy(c).validate(); err != nil {
			t.Fatal("valid resource exports", err)
		}
		for name, mutate := range map[string]func(*Compiled){
			"memory-index":          func(v *Compiled) { v.memoryDir.exports["bad"] = 2 },
			"global-index":          func(v *Compiled) { v.GlobalExports["bad"] = 3 },
			"global-escape":         func(v *Compiled) { v.Globals[2].Type = ValFuncRef },
			"function-global-name":  func(v *Compiled) { v.GlobalExports["run"] = 1 },
			"function-memory-name":  func(v *Compiled) { v.memoryDir.exports["run"] = 0 },
			"global-memory-name":    func(v *Compiled) { v.memoryDir.exports["counter"] = 0 },
			"empty-cross-kind-name": func(v *Compiled) { v.memoryDir.exports[""] = 0; v.GlobalExports[""] = 1 },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				copy := rv64ReferenceResourceCopy(c)
				mutate(copy)
				if err := copy.validate(); err == nil {
					t.Fatal("malformed resource exports admitted")
				}
				blob, err := marshalCompiled(copy)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed resource artifact loaded")
				}
			})
		}
		public := rv64ReferenceResourceCopy(c)
		public.Exports["reference"] = 2
		if err := public.validate(); err != nil {
			t.Fatal("ephemeral resource-profile reference export", err)
		}
		blob, err := marshalCompiled(public)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal("ephemeral resource-profile reference artifact", err)
		}
		loaded.Close()
		copy := rv64ReferenceResourceCopy(c)
		copy.memoryDir.exports[""] = 1
		if err := copy.validate(); err != nil {
			t.Fatal("valid empty export name", err)
		}
		if a := testing.AllocsPerRun(10, func() {
			remaining := railshot.MaxModuleBytes
			if rv64ValidateReferenceFunctionExports(copy, &remaining) != nil {
				panic("resource export preflight")
			}
		}); a != 0 {
			t.Fatal("resource export preflight allocations", a)
		}
		for len(copy.Exports)+len(copy.GlobalExports)+len(copy.memoryDir.exports) < railshot.MaxFunctions {
			name := fmt.Sprintf("resource%d", len(copy.memoryDir.exports))
			copy.memoryDir.exports[name] = 0
		}
		if err := copy.validate(); err != nil {
			t.Fatal("exact combined export count", err)
		}
		copy.GlobalExports["overflow"] = 1
		if err := rv64ValidateReferenceFunctions(copy); err == nil || !strings.Contains(err.Error(), "export count bound") {
			t.Fatal("separate map counts escaped aggregate bound", err)
		}
		copy = rv64ReferenceResourceCopy(c)
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
			t.Fatal("exact combined resource name/state/type budget")
		}
		delete(copy.GlobalExports, "counter-alias")
		copy.GlobalExports["counter-alias!"] = 1
		if err := rv64ValidateReferenceFunctions(copy); err == nil || !strings.Contains(err.Error(), "aggregate declared carrier byte bound") {
			t.Fatal("global export name not charged", err)
		}
		delete(copy.GlobalExports, "counter-alias!")
		copy.GlobalExports["counter-alias"] = 1
		delete(copy.memoryDir.exports, "alias0")
		copy.memoryDir.exports["alias0!"] = 0
		if err := rv64ValidateReferenceFunctions(copy); err == nil || !strings.Contains(err.Error(), "aggregate declared carrier byte bound") {
			t.Fatal("memory export name not charged", err)
		}
	}
}
