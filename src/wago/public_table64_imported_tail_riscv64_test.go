//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ImportedTable64IndirectTailSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("tail", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x13, 0, 0, 0x0b}))),
	)
}

func TestRV64PublicImportedTable64IndirectTail(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64NativeTailCompile(t, "imported-table64-tail-producer", rv64Table64IndirectInstructionSource(), artifact)
			consumerCode := rv64NativeTailCompile(t, "imported-table64-tail-consumer", rv64ImportedTable64IndirectTailSource(), artifact)
			if len(consumerCode.code) != 1436 || sha256.Sum256(consumerCode.code) != [32]byte{0xa9, 0x67, 0xe6, 0x62, 0xfd, 0xd6, 0x5c, 0x25, 0x37, 0x1d, 0xd0, 0x96, 0x73, 0x16, 0x14, 0x00, 0x7c, 0x39, 0x98, 0x6d, 0xa3, 0x11, 0x30, 0xa1, 0x7b, 0xa1, 0xbd, 0xa4, 0x5c, 0xbf, 0x47, 0xae} || !rv64ImportedTable64IndirectTail(consumerCode.executionView()) {
				t.Fatal("imported table64 tail metadata contract")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
			defer rv64HostCertificateCloseRuntime(t, rt)
			producer := rv64HostTailInstantiate(t, rt, producerCode, nil)
			table, err := producer.ExportedTable("table")
			if err != nil || table.Size() != 2 {
				t.Fatal("imported table64 tail producer table", table, err)
			}
			defer table.Close()
			consumer := rv64HostTailInstantiate(t, rt, consumerCode, NewImports().Table("env", "table", table))
			alias, err := consumer.ExportedTable("table")
			if err != nil || alias != table {
				t.Fatal("imported table64 tail export identity", err)
			}
			if err := consumerCode.Close(); err != nil {
				t.Fatal("close consumer compiled handle before tails", err)
			}

			checkTrap := func(index uint64) {
				t.Helper()
				before := slices.Clone(table.desc)
				got, callErr := consumer.Invoke("tail", index)
				var trap *coreruntime.TrapError
				if callErr == nil || got != nil || !errors.As(callErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 3 {
					t.Fatal("imported table64 tail index did not trap", index, got, callErr)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 2 {
					t.Fatal("imported table64 tail trap changed storage", index)
				}
				if state := &consumer.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || state.tableDepth != 0 || rv64EntryCapability(consumer) != 0 {
					t.Fatal("imported table64 tail leaked generation", index)
				}
			}
			checkTrap(0)
			for index := uint64(0); index < 2; index++ {
				if got, callErr := producer.Invoke("call", index); callErr != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("imported table64 tail producer seed", index, got, callErr)
				}
				if got, callErr := consumer.Invoke("tail", index); callErr != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("imported table64 proper tail", index, got, callErr)
				}
			}
			for _, index := range []uint64{2, 1<<32 | 1, ^uint64(0)} {
				checkTrap(index)
				if got, callErr := consumer.Invoke("tail", 1); callErr != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("imported table64 tail reuse after trap", index, got, callErr)
				}
			}

			if err := producerCode.Close(); err != nil {
				t.Fatal("close producer code before retained tail", err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal("close producer before retained tail", err)
			}
			fn, err := consumer.WasmFunc("tail")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if got, callErr := session.Invoke(1); callErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("prepared imported table64 tail after producer close", got, callErr)
			}
		})
	}
}

func TestRV64PublicImportedTable64IndirectTailGates(t *testing.T) {
	base := rv64ImportedTable64IndirectTailSource()
	for name, source := range map[string][]byte{
		"table32":       bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"different-min": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x05, 2, 4}, 1),
		"wrong-table":   bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 0}, []byte{0x20, 0, 0x13, 0, 1}, 1),
		"wrong-type":    bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 0}, []byte{0x20, 0, 0x13, 1, 0}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := rv64NativeTailConfig().Compile(source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby imported table64 tail source admitted")
			}
		})
	}

	compiled := rv64NativeTailCompile(t, "imported-table64-tail-gates", base, false)
	for name, mutate := range map[string]func(*Compiled){
		"missing-tail-feature": func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall },
		"missing-import":       func(c *Compiled) { c.tableImport = "" },
		"minimum":              func(c *Compiled) { c.tableImportMin++ },
		"entry":                func(c *Compiled) { c.Entry[0]++ },
		"function-key":         func(c *Compiled) { c.FuncTypeID[0] ^= 1 },
		"gc-descriptor":        func(c *Compiled) { c.GCTypeDescs[0].Final = false },
		"plan-home":            func(c *Compiled) { rv64NativeTailModule(c).Plans[0].ReferenceBase = 48 },
		"plan-source":          func(c *Compiled) { rv64NativeTailModule(c).Plans[0].SourcePC++ },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Entry = slices.Clone(bad.Entry)
			bad.InternalEntry = slices.Clone(bad.InternalEntry)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(bad)
			if rv64ImportedTable64IndirectTail(bad) || railshotValidateCompiled(bad) == nil {
				t.Fatal("hostile imported table64 tail metadata admitted")
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
				t.Fatal("hostile imported table64 tail artifact admitted")
			}
		})
	}
}
