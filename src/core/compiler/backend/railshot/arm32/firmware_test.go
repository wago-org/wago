package arm32

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

type armFirmwareInvoker struct{}

func (armFirmwareInvoker) Instantiate(uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (armFirmwareInvoker) Start(uint32, uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (armFirmwareInvoker) Call(uint32, uint32, []uint32, []uint32) embedded32.TransportCode {
	return embedded32.TransportCodeOK
}
func (armFirmwareInvoker) Cancel(uint32) embedded32.TransportCode { return embedded32.TransportCodeOK }
func (armFirmwareInvoker) Reset(uint32) embedded32.TransportCode  { return embedded32.TransportCodeOK }

func TestNewFirmwareTransportRunner(t *testing.T) {
	functions := []embedded32.FirmwareTransportFunction{{Address: 0x20000101, ParamSlots: 1, ResultSlots: 2}}
	image := &shared.EmbeddedFirmwareImage{ContextAddress: 0x20000040, StartAddress: 0x20000181, TransportFunctions: functions}
	runner, err := NewFirmwareTransportRunner(image, 1024, armFirmwareInvoker{})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Target != embedded32.TransportTargetArm32 || runner.ContextAddress != image.ContextAddress || runner.StartAddress != image.StartAddress || len(runner.Functions) != 1 || runner.Functions[0] != functions[0] {
		t.Fatalf("runner=%+v", runner)
	}
	if _, err := NewFirmwareTransportRunner(nil, 1024, armFirmwareInvoker{}); err == nil {
		t.Fatal("nil image accepted")
	}
}
