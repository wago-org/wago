//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64OwnedTable64IndirectTailSource() []byte {
	declaredTarget := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 2})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("seed", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("tail", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(9, wasmtest.Vec(declaredTarget)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x41, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x13, 0, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicOwnedTable64IndirectTail(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "owned-table64-indirect-tail", rv64OwnedTable64IndirectTailSource(), artifact)
			if !rv64SourceContractValid(c.executionView()) || rv64NativeTailModule(c) == nil || len(rv64NativeTailModule(c).Plans) != 1 {
				t.Fatal("owned table64 tail metadata contract")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64HostTailInstantiate(t, rt, c, nil)
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 2 {
				t.Fatal("owned table64 tail export", table, err)
			}
			defer table.Close()
			if err := c.Close(); err != nil {
				t.Fatal("close compiled handle before owned table64 tails", err)
			}

			checkTrap := func(index uint64) {
				t.Helper()
				before := slices.Clone(table.desc)
				got, callErr := in.Invoke("tail", index)
				var trap *coreruntime.TrapError
				if callErr == nil || got != nil || !errors.As(callErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 2 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 3 {
					t.Fatal("owned table64 tail index did not trap", index, got, callErr)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 2 {
					t.Fatal("owned table64 tail trap changed storage", index)
				}
				if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || state.tableDepth != 0 || rv64EntryCapability(in) != 0 {
					t.Fatal("owned table64 tail leaked generation", index)
				}
			}
			checkTrap(0)
			for index := uint64(0); index < 2; index++ {
				if got, callErr := in.Invoke("seed", index); callErr != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("owned table64 tail seed", index, got, callErr)
				}
				if got, callErr := in.Invoke("tail", index); callErr != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("owned table64 proper tail", index, got, callErr)
				}
			}
			for _, index := range []uint64{2, 1<<32 | 1, ^uint64(0)} {
				checkTrap(index)
				if got, callErr := in.Invoke("tail", 1); callErr != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("owned table64 tail reuse", index, got, callErr)
				}
			}
			fn, err := in.WasmFunc("tail")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if got, callErr := session.Invoke(1); callErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("prepared owned table64 tail", got, callErr)
			}
		})
	}
}

func TestRV64PublicOwnedTable64IndirectTailGates(t *testing.T) {
	base := rv64OwnedTable64IndirectTailSource()
	for name, source := range map[string][]byte{
		"table32":      bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 2, 2}, []byte{0x70, 0x01, 2, 2}, 1),
		"table-limit":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 2, 2}, []byte{0x70, 0x05, 2, 3}, 1),
		"tail-nontail": bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 0}, []byte{0x20, 0, 0x11, 0, 0}, 1),
		"tail-table":   bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 0}, []byte{0x20, 0, 0x13, 0, 1}, 1),
		"seed-table":   bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0xd2, 0, 0x26, 0}, []byte{0x20, 0, 0xd2, 0, 0x26, 1}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := rv64NativeTailConfig().Compile(source)
			if name == "table-limit" || name == "tail-nontail" {
				if err != nil {
					t.Fatal("valid table64 tail variant", err)
				}
				defer c.Close()
				rv64RequireTableSourceArtifact(t, c)
			} else {
				if c != nil {
					c.Close()
				}
				if err == nil {
					t.Fatal("invalid table64 index or width admitted")
				}
			}
		})
	}

	compiled := rv64NativeTailCompile(t, "owned-table64-indirect-tail-gates", base, false)
	for name, mutate := range map[string]func(*Compiled){
		"missing-tail-feature": func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall },
		"table-size":           func(c *Compiled) { c.TableSize-- },
		"entry":                func(c *Compiled) { c.Entry[2]-- },
		"function-key":         func(c *Compiled) { c.FuncTypeID[2] ^= 1 },
		"declared-caller": func(c *Compiled) {
			i32, _ := valueTypeDescriptorFromValType(ValI32)
			c.Types[1].Params[0] = i32
		},
		"gc-descriptor": func(c *Compiled) { c.GCTypeDescs[0].Final = false },
		"plan-function": func(c *Compiled) { rv64NativeTailModule(c).Plans[0].FunctionIndex = 1 },
		"plan-home":     func(c *Compiled) { rv64NativeTailModule(c).Plans[0].ReferenceBase = 48 },
		"plan-source":   func(c *Compiled) { rv64NativeTailModule(c).Plans[0].SourcePC++ },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Types = slices.Clone(bad.Types)
			bad.Types[1].Params = slices.Clone(bad.Types[1].Params)
			bad.Entry = slices.Clone(bad.Entry)
			bad.InternalEntry = slices.Clone(bad.InternalEntry)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile owned table64 tail metadata admitted")
			}

		})
	}
}
