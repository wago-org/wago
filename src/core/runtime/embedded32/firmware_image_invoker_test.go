package embedded32

import (
	"encoding/binary"
	"slices"
	"testing"
)

type testFirmwareNative struct {
	descriptor *FirmwareImageDescriptor
	starts     int
	calls      int
	address    uint32
	context    uint32
	trapAtCall uint32
	code       TransportCode
}

func (n *testFirmwareNative) Start(address, context uint32) TransportCode {
	n.starts++
	n.address, n.context = address, context
	n.trapAtCall = firmwareTestCell(n.descriptor, context, ContextTrapCellOffset)
	return n.code
}

func (n *testFirmwareNative) Call(address, context uint32, parameters, results []uint32) TransportCode {
	n.calls++
	n.address, n.context = address, context
	n.trapAtCall = firmwareTestCell(n.descriptor, context, ContextTrapCellOffset)
	if n.code != TransportCodeOK {
		return n.code
	}
	for index := range results {
		results[index] = uint32(index) + 40
		if index < len(parameters) {
			results[index] += parameters[index]
		}
	}
	return TransportCodeOK
}

func firmwareTestCell(d *FirmwareImageDescriptor, context, field uint32) uint32 {
	contextOffset, _ := d.rangeOffset(context, ContextABISize)
	address := binary.LittleEndian.Uint32(d.Image[contextOffset+field:])
	offset, _ := d.rangeOffset(address, 4)
	return binary.LittleEndian.Uint32(d.Image[offset:])
}

func newFirmwareImageFixture(target uint32) (*FirmwareImageDescriptor, []byte) {
	const base = uint32(0x20000000)
	initial := make([]byte, 256)
	contexts := []uint32{base + 32, base + 112}
	for index, context := range contexts {
		offset := context - base
		binary.LittleEndian.PutUint32(initial[offset+ContextTrapCellOffset:], base+200+uint32(index)*4)
		binary.LittleEndian.PutUint32(initial[offset+ContextCancelCellOffset:], base+216+uint32(index)*4)
		binary.LittleEndian.PutUint32(initial[offset+ContextHelperTableOffset:], base+160+uint32(index)*16)
		binary.LittleEndian.PutUint32(initial[200+index*4:], 99)
	}
	callMask := uint32(0)
	if target == TransportTargetArm32 {
		callMask = 1
	}
	d := &FirmwareImageDescriptor{
		Target:         target,
		MaximumPayload: 128,
		ImageAddress:   base,
		InitialImage:   string(initial),
		Image:          make([]byte, len(initial)),
		ContextAddress: contexts[0],
		StartAddress:   base + 8 | callMask,
		Functions: []FirmwareTransportFunction{
			{Address: base + 16 | callMask, Context: contexts[1], ParamSlots: 2, ResultSlots: 2},
		},
		Contexts:      contexts,
		HelperEntries: [4]uint32{0x1001, 0x2001, 0x3001, 0x4001},
	}
	return d, initial
}

func TestFirmwareImageInvokerLifecycle(t *testing.T) {
	d, initial := newFirmwareImageFixture(TransportTargetArm32)
	native := &testFirmwareNative{descriptor: d}
	published := 0
	invoker := &FirmwareImageInvoker{
		Descriptor: d,
		Native:     native,
		Publish: func(address uint32, image []byte) {
			published++
			if address != d.ImageAddress || len(image) != len(initial) {
				t.Fatalf("publish address=%#x bytes=%d", address, len(image))
			}
		},
	}
	runner := &FirmwareTransportRunner{
		Target: d.Target, MaximumPayload: d.MaximumPayload,
		ContextAddress: d.ContextAddress, StartAddress: d.StartAddress,
		Functions: d.Functions, Invoker: invoker,
	}
	if code := runner.Instantiate(); code != TransportCodeOK || published != 1 {
		t.Fatalf("instantiate=%#x published=%d", code, published)
	}
	for index, context := range d.Contexts {
		contextOffset, _ := d.rangeOffset(context, ContextABISize)
		helperAddress := binary.LittleEndian.Uint32(d.Image[contextOffset+ContextHelperTableOffset:])
		helperOffset, _ := d.rangeOffset(helperAddress, HelperTableBytes)
		got := [4]uint32{
			binary.LittleEndian.Uint32(d.Image[helperOffset+HelperF64Offset:]),
			binary.LittleEndian.Uint32(d.Image[helperOffset+HelperSIMDOffset:]),
			binary.LittleEndian.Uint32(d.Image[helperOffset+HelperI64Offset:]),
			binary.LittleEndian.Uint32(d.Image[helperOffset+HelperF32Offset:]),
		}
		if got != d.HelperEntries {
			t.Fatalf("context %d helpers=%#x", index, got)
		}
	}
	if code := runner.Start(); code != TransportCodeOK || native.starts != 1 ||
		native.address != d.StartAddress || native.context != d.ContextAddress || native.trapAtCall != 0 {
		t.Fatalf("start=%#x native=%+v", code, native)
	}
	results := make([]uint32, 2)
	if code := runner.Call(0, []uint32{1, 2}, results); code != TransportCodeOK ||
		!slices.Equal(results, []uint32{41, 43}) || native.calls != 1 ||
		native.context != d.Contexts[1] || native.trapAtCall != 0 {
		t.Fatalf("call=%#x results=%v native=%+v", code, results, native)
	}
	if code := runner.Cancel(); code != TransportCodeOK ||
		firmwareTestCell(d, d.ContextAddress, ContextCancelCellOffset) != 1 {
		t.Fatalf("cancel=%#x", code)
	}
	d.Image[0] = 0xff
	if code := runner.Reset(); code != TransportCodeOK || published != 2 || d.Image[0] != initial[0] {
		t.Fatalf("reset=%#x published=%d first=%#x", code, published, d.Image[0])
	}
}

func TestFirmwareImageInvokerPreflightsBeforeMutation(t *testing.T) {
	d, _ := newFirmwareImageFixture(TransportTargetRISCV32)
	for index := range d.Image {
		d.Image[index] = 0xa5
	}
	before := slices.Clone(d.Image)
	initial := []byte(d.InitialImage)
	second := d.Contexts[1] - d.ImageAddress
	binary.LittleEndian.PutUint32(initial[second+ContextHelperTableOffset:], 0x30000000)
	d.InitialImage = string(initial)
	invoker := &FirmwareImageInvoker{Descriptor: d, Native: &testFirmwareNative{descriptor: d}}
	if code := invoker.Instantiate(d.ContextAddress); code != TransportCodeState {
		t.Fatalf("instantiate=%#x", code)
	}
	if !slices.Equal(d.Image, before) {
		t.Fatal("invalid image mutated destination")
	}
}

func TestFirmwareImageInvokerRestoreAllocations(t *testing.T) {
	d, _ := newFirmwareImageFixture(TransportTargetRISCV32)
	invoker := &FirmwareImageInvoker{Descriptor: d, Native: &testFirmwareNative{descriptor: d}}
	allocations := testing.AllocsPerRun(100, func() {
		if code := invoker.Reset(d.ContextAddress); code != TransportCodeOK {
			panic(code)
		}
	})
	if allocations != 0 {
		t.Fatalf("restore allocations=%v", allocations)
	}
}
