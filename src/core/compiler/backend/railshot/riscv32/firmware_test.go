package riscv32

import (
	"os/exec"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type riscvFirmwareInvoker struct{}

func (riscvFirmwareInvoker) Instantiate(uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (riscvFirmwareInvoker) Start(uint32, uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (riscvFirmwareInvoker) Call(uint32, uint32, []uint32, []uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (riscvFirmwareInvoker) Cancel(uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (riscvFirmwareInvoker) Reset(uint32) embedded32.TransportCode { return embedded32.TransportCodeOK }

func TestLinkedFirmwareCallsProviderTableContextUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	for _, trapping := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "trap"}[trapping], func(t *testing.T) {
			providerBody := []byte{0x23, 0, 0x0b}
			if trapping {
				providerBody = []byte{0x00, 0x0b}
			}
			providerModule, err := wasm.DecodeModule(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 1, 1})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 42, 0x0b}))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
				wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(providerBody))),
			))
			if err != nil {
				t.Fatal(err)
			}
			provider, err := CompileModule(providerModule)
			if err != nil {
				t.Fatal(err)
			}
			tableImport := append(wasmtest.Name("provider"), wasmtest.Name("table")...)
			tableImport = append(tableImport, byte(wasm.ExternTable), 0x70, 1, 1, 1)
			consumerModule, err := wasm.DecodeModule(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(tableImport)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 1, 0x0b}))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x11, 0, 0, 0x23, 0, 0x6a, 0x0b}))),
			))
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := CompileModule(consumerModule)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := shared.ResolveEmbeddedLinks([]shared.EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
			if err != nil {
				t.Fatal(err)
			}
			const imageBase = uint32(0x20000)
			helpers := [4]uint32{1, 1, 1, 1}
			opts := LinkedFirmwareOptions{BaseAddress: imageBase, Modules: []shared.EmbeddedFirmwareOptions{{TableCapacity: 1, NativeStackLimit: 1, HelperEntries: helpers}, {TableCapacity: 1, NativeStackLimit: 1, HelperEntries: helpers}}}
			size, err := LinkedFirmwareImageSize(plan, opts)
			if err != nil {
				t.Fatal(err)
			}
			image, err := BuildLinkedFirmwareImage(make([]byte, size), plan, opts)
			if err != nil {
				t.Fatal(err)
			}
			consumerImage := image.Modules[1].Image
			entry := consumerImage.CodeAddress + consumer.Functions[0].Offset
			var a rv.Asm
			a.MovImm32(rv.X23, consumerImage.ContextAddress)
			a.MovImm32(rv.T0, entry)
			a.Blr(rv.T0)
			if trapping {
				a.MovImm32(rv.T0, consumerImage.TrapAddress)
				a.Lw(rv.A0, rv.T0, 0)
			}
			a.MovImm32(rv.A7, 93)
			a.Ecall()
			code := append([]byte(nil), a.B...)
			for uint32(len(code)) < imageBase-0x10000 {
				code = append(code, 0)
			}
			code = append(code, image.Bytes...)
			want := 43
			if trapping {
				want = int(embedded32.TrapUnreachable)
			}
			runRV32Exit(t, qemu, code, want)
		})
	}
}

func TestNewFirmwareTransportRunner(t *testing.T) {
	functions := []embedded32.FirmwareTransportFunction{{Address: 0x20000100, ParamSlots: 1, ResultSlots: 2}}
	image := &shared.EmbeddedFirmwareImage{ContextAddress: 0x20000040, StartAddress: 0x20000180, TransportFunctions: functions}
	runner, err := NewFirmwareTransportRunner(image, 1024, riscvFirmwareInvoker{})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Target != embedded32.TransportTargetRISCV32 || runner.ContextAddress != image.ContextAddress || runner.StartAddress != image.StartAddress || len(runner.Functions) != 1 || runner.Functions[0] != functions[0] {
		t.Fatalf("runner=%+v", runner)
	}
	if _, err := NewFirmwareTransportRunner(nil, 1024, riscvFirmwareInvoker{}); err == nil {
		t.Fatal("nil image accepted")
	}
}
