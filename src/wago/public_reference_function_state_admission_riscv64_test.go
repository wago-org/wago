//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceStateCopy(c *Compiled) *Compiled {
	copy := *rv64ReferenceImportTailCopy(c)
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
