//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64TwoTable64IndirectTailSource() []byte {
	declaredTarget := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	seed := func(table byte) []byte { return []byte{0x20, 0, 0xd2, 0, 0x26, table, 0x41, 1, 0x0b} }
	tail := func(table byte) []byte { return []byte{0x20, 0, 0x13, 0, table, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1),
			wasmtest.ULEB(1), wasmtest.ULEB(1),
		)),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x05, 2, 2},
			[]byte{0x70, 0x05, 3, 3},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("seed0", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("seed1", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("tail0", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("tail1", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(declaredTarget)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code(seed(0)),
			wasmtest.Code(seed(1)),
			wasmtest.Code(tail(0)),
			wasmtest.Code(tail(1)),
		)),
	)
}

func TestRV64PublicOwnedTwoTable64IndirectTail(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "owned-two-table64-indirect-tail", rv64TwoTable64IndirectTailSource(), artifact)
			if len(c.code) != 6156 || sha256.Sum256(c.code) != [32]byte{0x44, 0x5d, 0xdd, 0x59, 0x50, 0x60, 0x90, 0x83, 0x8a, 0x71, 0xb0, 0x7a, 0xba, 0xfe, 0xbc, 0x94, 0x19, 0x41, 0xe1, 0xfd, 0x79, 0xd9, 0xc4, 0xab, 0xa4, 0xbf, 0xcd, 0xb5, 0xd5, 0x28, 0x4d, 0x69} {
				t.Fatal("indexed table64 tail code image contract")
			}
			if !rv64OwnedTwoTable64IndirectTail(c.executionView()) || c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations|CoreFeatureTailCall {
				t.Fatal("indexed table64 tail metadata contract")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64HostTailInstantiate(t, rt, c, nil)
			table0, err := in.ExportedTable("table0")
			if err != nil {
				t.Fatal(err)
			}
			defer table0.Close()
			table1, err := in.ExportedTable("table1")
			if err != nil {
				t.Fatal(err)
			}
			defer table1.Close()
			for _, tc := range []struct {
				name     string
				function uint32
			}{{"tail0", 3}, {"tail1", 4}} {
				got, callErr := in.Invoke(tc.name, 0)
				var trap *coreruntime.TrapError
				if callErr == nil || got != nil || !errors.As(callErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function {
					t.Fatal("null indexed table64 tail did not trap", tc, got, callErr)
				}
			}

			for _, tc := range []struct {
				seed, tail string
				index      uint64
				other      *Table
			}{{"seed1", "tail1", 2, table0}, {"seed0", "tail0", 1, table1}} {
				other := slices.Clone(tc.other.desc)
				if got, callErr := in.Invoke(tc.seed, tc.index); callErr != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("indexed table64 tail seed", tc, got, callErr)
				}
				if got, callErr := in.Invoke(tc.tail, tc.index); callErr != nil || !slices.Equal(got, []uint64{7}) || !slices.Equal(tc.other.desc, other) {
					t.Fatal("indexed table64 proper tail", tc, got, callErr)
				}
				if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || state.tableDepth != 0 || rv64EntryCapability(in) != 0 {
					t.Fatal("indexed table64 tail leaked generation")
				}
			}

			before0, before1 := slices.Clone(table0.desc), slices.Clone(table1.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				index    uint64
			}{{"tail0", 3, 1<<32 | 1}, {"tail1", 4, 1<<32 | 2}, {"tail1", 4, ^uint64(0)}, {"tail0", 3, 2}, {"tail1", 4, 3}} {
				got, callErr := in.Invoke(tc.name, tc.index)
				var trap *coreruntime.TrapError
				if callErr == nil || got != nil || !errors.As(callErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function {
					t.Fatal("indexed table64 tail index did not trap", tc, got, callErr)
				}
				if !slices.Equal(table0.desc, before0) || !slices.Equal(table1.desc, before1) {
					t.Fatal("indexed table64 tail trap changed storage", tc)
				}
			}
			if got, callErr := in.Invoke("tail1", 2); callErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("indexed table64 tail reuse after trap", got, callErr)
			}
			fn, err := in.WasmFunc("tail1")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if got, callErr := session.Invoke(2); callErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("prepared indexed table64 tail", got, callErr)
			}
			session.Close()
		})
	}
}

func TestRV64PublicOwnedTwoTable64IndirectTailGates(t *testing.T) {
	base := rv64TwoTable64IndirectTailSource()
	for name, source := range map[string][]byte{
		"table1-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"tail1-table0":   bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 1}, []byte{0x20, 0, 0x13, 0, 0}, 1),
		"tail1-nontail":  bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 1}, []byte{0x20, 0, 0x11, 0, 1}, 1),
		"seed1-table0":   bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0xd2, 0, 0x26, 1}, []byte{0x20, 0, 0xd2, 0, 0x26, 0}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := rv64NativeTailConfig().Compile(source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby indexed table64 tail source admitted")
			}
		})
	}

	compiled := rv64NativeTailCompile(t, "owned-two-table64-indirect-tail-gates", base, false)
	for name, mutate := range map[string]func(*Compiled){
		"missing-tail-feature": func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall },
		"table-size":           func(c *Compiled) { c.extraTables[0].Size-- },
		"entry":                func(c *Compiled) { c.Entry[4]-- },
		"plan-function":        func(c *Compiled) { rv64NativeTailModule(c).Plans[1].FunctionIndex = 3 },
		"function-key":         func(c *Compiled) { c.FuncTypeID[4] ^= 1 },
		"plan-home":            func(c *Compiled) { rv64NativeTailModule(c).Plans[1].ReferenceBase = 48 },
		"plan-source":          func(c *Compiled) { rv64NativeTailModule(c).Plans[1].SourcePC++ },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Entry = slices.Clone(bad.Entry)
			bad.InternalEntry = slices.Clone(bad.InternalEntry)
			mutate(bad)
			if rv64OwnedTwoTable64IndirectTail(bad) || railshotValidateCompiled(bad) == nil {
				t.Fatal("hostile indexed table64 tail metadata admitted")
			}
			blob, err := bad.MarshalBinary()
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile indexed table64 tail artifact admitted")
			}
		})
	}
}
