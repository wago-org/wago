//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicTableCapacityUsesInstanceQuota(t *testing.T) {
	table := append([]byte{0x70, 1}, wasmtest.ULEB(2048)...)
	table = append(table, wasmtest.ULEB(2048)...)
	source := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))))
	for _, limit := range []uint64{1024, 1 << 20} {
		code, err := NewRuntimeConfig().WithMaxInstanceMetadataBytes(limit).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		defer code.Close()
		in, err := Instantiate(code)
		if limit == 1024 {
			var quota *coreruntime.ResourceLimitError
			if !errors.As(err, &quota) {
				t.Fatal("small instance quota did not bound table allocation", err)
			}
			if in != nil {
				in.Close()
			}
			continue
		}
		if err != nil {
			t.Fatal("configured quota permits larger table", err)
		}
		defer in.Close()
	}
}

func rv64LargeLocalSource() []byte {
	function := func(store bool) []byte {
		body := append([]byte{1}, wasmtest.ULEB(1055)...)
		body = append(body, 0x7e)
		if store {
			body = append(body, 0x20, 0, 0x21)
			body = append(body, wasmtest.ULEB(1055)...)
		}
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(1055)...)
		body = append(body, 0x0b)
		return append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0), wasmtest.ExportEntry("zero", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(function(true), function(false))))
}

func TestRV64PublicLargeLocalsRespectConfiguredLimitAndNativeHomes(t *testing.T) {
	source := rv64LargeLocalSource()
	if code, err := NewRuntimeConfig().WithMaxFunctionLocals(1055).Compile(source); code != nil || err == nil || !strings.Contains(err.Error(), "configured limit") {
		t.Fatal("configured local ceiling was not enforced", err)
	}
	for _, artifact := range []bool{false} {
		code, err := NewRuntimeConfig().WithMaxFunctionLocals(1056).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		defer code.Close()

		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		const value = uint64(0xfedcba9876543210)
		for range 4 {
			for _, test := range []struct {
				name string
				want uint64
			}{{"store", value}, {"zero", 0}} {
				got, err := in.Invoke(test.name, value)
				if err != nil || len(got) != 1 || got[0] != test.want {
					t.Fatalf("artifact=%v %s: %v, %v", artifact, test.name, got, err)
				}
			}
		}
	}
}

func BenchmarkRV64PublicLargeLocalInvoke(b *testing.B) {
	code, err := NewRuntimeConfig().WithMaxFunctionLocals(1056).Compile(rv64LargeLocalSource())
	if err != nil {
		b.Fatal(err)
	}
	defer code.Close()
	in, err := Instantiate(code)
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := in.Invoke("zero", 42)
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatal(got, err)
		}
	}
}

func TestRV64PublicLargeLocalZeroLoopBoundaries(t *testing.T) {
	for _, locals := range []uint32{256, 257, 262, 263, 264} {
		makeCode := func(fill bool) []byte {
			body := append([]byte{1}, wasmtest.ULEB(locals)...)
			body = append(body, 0x7e)
			for i := uint32(1); i <= locals; i++ {
				if fill {
					body = append(body, 0x20, 0, 0x21)
					body = append(body, wasmtest.ULEB(i)...)
				}
				body = append(body, 0x20)
				body = append(body, wasmtest.ULEB(i)...)
				if i != 1 {
					body = append(body, 0x84) // i64.or
				}
			}
			body = append(body, 0x0b)
			return append(wasmtest.ULEB(uint32(len(body))), body...)
		}
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
			wasmtest.Section(3, []byte{2, 0, 0}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fill", 0, 0), wasmtest.ExportEntry("scan", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(makeCode(true), makeCode(false))))
		code, err := NewRuntimeConfig().WithMaxFunctionLocals(locals + 1).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(code)
		if err != nil {
			code.Close()
			t.Fatal(err)
		}
		for iteration := range 4 {
			const value = uint64(0xfedcba9876543210)
			for _, test := range []struct {
				name string
				want uint64
			}{{"fill", value}, {"scan", 0}} {
				got, err := in.Invoke(test.name, value)
				if err != nil || len(got) != 1 || got[0] != test.want {
					t.Fatalf("%d locals, iteration %d, %s: %v, %v", locals, iteration, test.name, got, err)
				}
			}
		}
		in.Close()
		code.Close()
	}
}
