//go:build amd64

package wago

import (
	"strings"
	"testing"

	"golang.org/x/sys/cpu"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func bitCountModule(op byte, width wasm.ValType) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{width}, []wasm.ValType{width}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, op, 0x0b}))),
	)
}

func TestAMD64BitCountHostGate(t *testing.T) {
	oldBMI1, oldPOPCNT := cpu.X86.HasBMI1, cpu.X86.HasPOPCNT
	defer func() {
		cpu.X86.HasBMI1, cpu.X86.HasPOPCNT = oldBMI1, oldPOPCNT
	}()

	for _, tc := range []struct {
		name string
		op   byte
		flag *bool
	}{
		{name: "missing BMI1 for ctz", op: 0x68, flag: &cpu.X86.HasBMI1},
		{name: "missing POPCNT", op: 0x69, flag: &cpu.X86.HasPOPCNT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := *tc.flag
			*tc.flag = false
			defer func() { *tc.flag = old }()
			compiled, err := Compile(nil, bitCountModule(tc.op, wasm.I32))
			if compiled != nil {
				defer compiled.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "bit-count") {
				t.Fatalf("compile without %s = %v, want bit-count CPU error", tc.name, err)
			}
		})
	}
}

func BenchmarkAMD64BitCountCompile(b *testing.B) {
	module := bitCountModule(0x68, wasm.I32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(nil, module)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
}

func BenchmarkAMD64BitCountInvoke(b *testing.B) {
	compiled, err := Compile(nil, bitCountModule(0x68, wasm.I32))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := instance.Invoke("f", 8); err != nil {
			b.Fatal(err)
		}
	}
}
