//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
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

func rv64Table64ActiveCallableElementSource(offset byte) []byte {
	active := []byte{0x04, 0x42, offset, 0x0b, 0x01, 0xd2, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(9, wasmtest.Vec(active)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
	)
}

func TestRV64PublicTable64ActiveElement(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "table64-active-element", rv64Table64ActiveElementSource(1), artifact)
			defer c.Close()
			if !rv64SourceContractValid(c.executionView()) {
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

func TestRV64PublicTable64ActiveElementIndirectLifetime(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "table64-active-callable-producer", rv64Table64ActiveCallableElementSource(1), artifact)
			consumerCode := rv64TableStageCompile(t, "table64-active-callable-consumer", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer consumerCode.Close()
			if !rv64SourceContractValid(producerCode.executionView()) || !rv64SourceContractValid(consumerCode.executionView()) {
				t.Fatal("table64 active call composition metadata contract")
			}
			producer, err := Instantiate(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			table, err := producer.ExportedTable("table")
			if err != nil || table.Size() != 3 {
				t.Fatal("active table export", table, err)
			}
			defer table.Close()
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			call := func() {
				t.Helper()
				got, err := consumer.Invoke("call", 1)
				if err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("active table64 indirect call", got, err)
				}
				for _, index := range []uint64{0, 2} {
					got, err := consumer.Invoke("call", index)
					var trap *coreruntime.TrapError
					if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
						t.Fatal("active table64 null row did not trap", index, got, err)
					}
				}
			}
			call()
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			call()
		})
	}
}

func TestRV64PublicTable64ActiveElementSourceGates(t *testing.T) {
	base := rv64Table64ActiveElementSource(1)
	callable := rv64Table64ActiveCallableElementSource(1)
	mutations := map[string][]byte{
		"offset":        rv64Table64ActiveElementSource(2),
		"table-size":    bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x05, 2, 3}, 1),
		"null":          bytes.Replace(bytes.Clone(base), []byte{0xd2, 0x00, 0x0b}, []byte{0xd0, 0x70, 0x0b}, 1),
		"body":          bytes.Replace(bytes.Clone(base), []byte{0x02, 0x00, 0x0b}, []byte{0x03, 0x00, 0x01, 0x0b}, 1),
		"callable-body": bytes.Replace(bytes.Clone(callable), []byte{0x41, 0x07, 0x0b}, []byte{0x41, 0x08, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name != "body" {
				if err != nil {
					t.Fatal("valid table variant", err)
				}
				defer c.Close()
				rv64RequireTableArtifactMetadata(t, c)
			} else {
				if c != nil {
					c.Close()
				}
				if err == nil {
					t.Fatal("invalid table index width admitted")
				}
			}
		})
	}
}
