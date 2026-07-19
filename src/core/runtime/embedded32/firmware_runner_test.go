package embedded32

import (
	"slices"
	"testing"
)

type testFirmwareInvoker struct {
	instantiateCode TransportCode
	startCode       TransportCode
	callCode        TransportCode
	cancelCode      TransportCode
	resetCode       TransportCode
	instantiates    int
	starts          int
	calls           int
	cancels         int
	resets          int
	lastAddress     uint32
	lastContext     uint32
}

func (i *testFirmwareInvoker) Instantiate(context uint32) TransportCode {
	i.instantiates++
	i.lastContext = context
	return i.instantiateCode
}
func (i *testFirmwareInvoker) Start(address, context uint32) TransportCode {
	i.starts++
	i.lastAddress, i.lastContext = address, context
	return i.startCode
}
func (i *testFirmwareInvoker) Call(address, context uint32, parameters, results []uint32) TransportCode {
	i.calls++
	i.lastAddress, i.lastContext = address, context
	if i.callCode != TransportCodeOK {
		return i.callCode
	}
	for n := range results {
		results[n] = uint32(n) + 40
		if n < len(parameters) {
			results[n] += parameters[n]
		}
	}
	return TransportCodeOK
}
func (i *testFirmwareInvoker) Cancel(context uint32) TransportCode {
	i.cancels++
	i.lastContext = context
	return i.cancelCode
}
func (i *testFirmwareInvoker) Reset(context uint32) TransportCode {
	i.resets++
	i.lastContext = context
	return i.resetCode
}

func testFirmwareRunner(invoker *testFirmwareInvoker) *FirmwareTransportRunner {
	return &FirmwareTransportRunner{
		Target:         TransportTargetRISCV32,
		MaximumPayload: 256,
		ContextAddress: 0x20000100,
		StartAddress:   0x20000200,
		Functions: []FirmwareTransportFunction{
			{Address: 0x20000300, ParamSlots: 2, ResultSlots: 2},
		},
		Invoker: invoker,
	}
}

func TestFirmwareTransportRunnerLifecycle(t *testing.T) {
	invoker := &testFirmwareInvoker{}
	runner := testFirmwareRunner(invoker)
	if code := runner.Call(0, []uint32{1, 2}, make([]uint32, 2)); code != TransportCodeState {
		t.Fatalf("early call code=%#x", code)
	}
	if code := runner.Instantiate(); code != TransportCodeOK || invoker.instantiates != 1 || invoker.lastContext != runner.ContextAddress {
		t.Fatalf("instantiate code=%#x invoker=%+v", code, invoker)
	}
	if code := runner.Instantiate(); code != TransportCodeState || invoker.instantiates != 1 {
		t.Fatalf("duplicate instantiate code=%#x count=%d", code, invoker.instantiates)
	}
	if code := runner.Start(); code != TransportCodeOK || invoker.starts != 1 || invoker.lastAddress != runner.StartAddress {
		t.Fatalf("start code=%#x invoker=%+v", code, invoker)
	}
	results := make([]uint32, 2)
	if code := runner.Call(0, []uint32{1, 2}, results); code != TransportCodeOK || !slices.Equal(results, []uint32{41, 43}) || invoker.calls != 1 || invoker.lastAddress != runner.Functions[0].Address {
		t.Fatalf("call code=%#x results=%v invoker=%+v", code, results, invoker)
	}
	if code := runner.Cancel(); code != TransportCodeOK || invoker.cancels != 1 {
		t.Fatalf("cancel code=%#x count=%d", code, invoker.cancels)
	}
	if code := runner.Reset(); code != TransportCodeOK || invoker.resets != 1 || runner.initialized || runner.started {
		t.Fatalf("reset code=%#x runner=%+v invoker=%+v", code, runner, invoker)
	}
	if code := runner.Start(); code != TransportCodeState {
		t.Fatalf("start after reset code=%#x", code)
	}
}

func TestFirmwareTransportRunnerPublishesFailuresTransactionally(t *testing.T) {
	invoker := &testFirmwareInvoker{instantiateCode: TransportCodeCapacity}
	runner := testFirmwareRunner(invoker)
	if code := runner.Instantiate(); code != TransportCodeCapacity || runner.initialized {
		t.Fatalf("instantiate code=%#x initialized=%v", code, runner.initialized)
	}
	invoker.instantiateCode = TransportCodeOK
	invoker.startCode = TransportTrapCode(TrapUnreachable)
	if code := runner.Instantiate(); code != TransportCodeOK {
		t.Fatal(code)
	}
	if code := runner.Start(); code != TransportTrapCode(TrapUnreachable) || runner.started {
		t.Fatalf("start code=%#x started=%v", code, runner.started)
	}
	invoker.startCode = TransportCodeOK
	if code := runner.Start(); code != TransportCodeOK || !runner.started {
		t.Fatalf("retry start code=%#x started=%v", code, runner.started)
	}
	invoker.callCode = TransportTrapCode(TrapMemoryOutOfBounds)
	if code := runner.Call(0, []uint32{1, 2}, make([]uint32, 2)); code != TransportTrapCode(TrapMemoryOutOfBounds) {
		t.Fatalf("call trap=%#x", code)
	}
	if code := runner.Call(0, []uint32{1}, make([]uint32, 2)); code != TransportCodeState || invoker.calls != 1 {
		t.Fatalf("shape code=%#x calls=%d", code, invoker.calls)
	}
	invoker.resetCode = TransportCodeCapacity
	if code := runner.Reset(); code != TransportCodeCapacity || !runner.initialized || !runner.started {
		t.Fatalf("reset code=%#x initialized=%v started=%v", code, runner.initialized, runner.started)
	}
}

func TestFirmwareTransportRunnerWithoutStartMarksReady(t *testing.T) {
	invoker := &testFirmwareInvoker{}
	runner := testFirmwareRunner(invoker)
	runner.StartAddress = 0
	if code := runner.Instantiate(); code != TransportCodeOK {
		t.Fatal(code)
	}
	if code := runner.Start(); code != TransportCodeOK || invoker.starts != 0 || !runner.started {
		t.Fatalf("start code=%#x starts=%d ready=%v", code, invoker.starts, runner.started)
	}
	hello := runner.Hello()
	if hello.Target != TransportTargetRISCV32 || hello.ContextABIBytes != ContextABISize || hello.CallABIBytes != CallABIBytes || hello.MaximumPayload != runner.MaximumPayload {
		t.Fatalf("hello=%+v", hello)
	}
}
