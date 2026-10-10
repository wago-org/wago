//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64Table64ActiveElementSource(offset byte) []byte {
	active := []byte{0x04, 0x42, offset, 0x0b, 0x01, 0xd2, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(9, wasmtest.Vec(active)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func TestRV64PublicTable64ActiveElement(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "table64-active-element", rv64Table64ActiveElementSource(1), artifact)
			defer c.Close()
			if !rv64OwnedTable64ActiveElement(c.executionView()) {
				t.Fatal("table64 active-element metadata contract")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			table, err := in.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			for index, wantNull := range []bool{true, false, true} {
				null, err := table.EntryIsNull(uint64(index))
				if err != nil || null != wantNull {
					t.Fatalf("table64 active row %d: null=%v, want %v, err=%v", index, null, wantNull, err)
				}
			}
		})
	}
}

func TestRV64PublicTable64ActiveElementSourceGates(t *testing.T) {
	base := rv64Table64ActiveElementSource(1)
	mutations := map[string][]byte{
		"offset":     rv64Table64ActiveElementSource(2),
		"table-size": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x05, 2, 3}, 1),
		"null":       bytes.Replace(bytes.Clone(base), []byte{0xd2, 0x00, 0x0b}, []byte{0xd0, 0x70, 0x0b}, 1),
		"body":       bytes.Replace(bytes.Clone(base), []byte{0x02, 0x00, 0x0b}, []byte{0x03, 0x00, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby table64 active-element source admitted")
			}
		})
	}
}

func TestRV64PublicTable64ActiveElementArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"feature":    func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTable64 },
		"witness":    func(c *Compiled) { c.nativeTable64ABI = nativeTable64SizeABI },
		"table32":    func(c *Compiled) { c.TableAddr64 = false },
		"capacity":   func(c *Compiled) { c.TableMax = 4 },
		"descriptor": func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"offset": func(c *Compiled) {
			c.Elems[0].Offset.Expr = []byte{0x42, 0x02, 0x0b}
		},
		"payload": func(c *Compiled) { c.Elems[0].Values[0].Null = true },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[0] ^= 1
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			base := rv64TableStageCompile(t, "table64-active-element-gates", rv64Table64ActiveElementSource(1), false)
			bad := cloneCompiledMetadata(base.executionView())
			bad.Elems = slices.Clone(bad.Elems)
			bad.Elems[0].Values = slices.Clone(bad.Elems[0].Values)
			bad.Elems[0].Offset.Expr = bytes.Clone(bad.Elems[0].Offset.Expr)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			mutate(bad)
			if rv64OwnedTable64ActiveElement(bad) || !errors.Is(railshotValidateCompiled(bad), railshot.ErrUnsupported) {
				t.Fatal("hostile table64 active-element metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "feature" {
				if err != nil || loaded == nil || !rv64OwnedTable64ActiveElement(loaded.executionView()) || !loaded.requiredFeatures.IsEnabled(CoreFeatureTable64) {
					t.Fatal("serialized table64 structural feature was not restored", err)
				}
				loaded.Close()
				return
			}
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile table64 active-element artifact admitted")
			}
		})
	}
}
