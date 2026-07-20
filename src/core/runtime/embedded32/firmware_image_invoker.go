package embedded32

import "encoding/binary"

// FirmwareImageDescriptor is the allocation-free Go representation of one
// prelinked bare-metal image. InitialImage is normally a generated string
// constant kept in flash. InitialImageBytes supports a caller-owned uploaded
// snapshot. Exactly one initial-image representation must be present. Image is
// caller-owned storage mapped at ImageAddress.
type FirmwareImageDescriptor struct {
	Target            uint32
	MaximumPayload    uint32
	ImageAddress      uint32
	InitialImage      string
	InitialImageBytes []byte
	Image             []byte

	ContextAddress uint32
	StartAddress   uint32
	Functions      []FirmwareTransportFunction
	Contexts       []uint32
	HelperEntries  [4]uint32
	StackLimit     uint32
}

// FirmwareNativeEntry is the minimal architecture-specific boundary required
// to enter generated 32-bit code. Implementations are target assembly, not cgo.
type FirmwareNativeEntry interface {
	Start(entryAddress, contextAddress uint32) TransportCode
	Call(entryAddress, contextAddress uint32, parameters, results []uint32) TransportCode
}

// FirmwareImageInvoker restores and patches a FirmwareImageDescriptor and then
// delegates only the actual machine-code entry to Native. Publish performs any
// target instruction synchronization after the complete image is ready.
type FirmwareImageInvoker struct {
	Descriptor *FirmwareImageDescriptor
	Native     FirmwareNativeEntry
	Publish    func(imageAddress uint32, image []byte)
}

// Valid reports whether every address and patch location is safe to use before
// the live image is mutated.
func (d *FirmwareImageDescriptor) Valid() bool { return d.valid() }

func (i *FirmwareImageInvoker) Instantiate(contextAddress uint32) TransportCode {
	if i == nil || i.Descriptor == nil || contextAddress != i.Descriptor.ContextAddress {
		return TransportCodeState
	}
	return i.restore()
}

func (i *FirmwareImageInvoker) Start(entryAddress, contextAddress uint32) TransportCode {
	if i == nil || i.Descriptor == nil || i.Native == nil || entryAddress == 0 ||
		!i.Descriptor.callable(entryAddress) || !i.clearTrap(contextAddress) {
		return TransportCodeState
	}
	return i.Native.Start(entryAddress, contextAddress)
}

func (i *FirmwareImageInvoker) Call(entryAddress, contextAddress uint32, parameters, results []uint32) TransportCode {
	if i == nil || i.Descriptor == nil || i.Native == nil || entryAddress == 0 ||
		!i.Descriptor.callable(entryAddress) || !i.clearTrap(contextAddress) {
		return TransportCodeState
	}
	return i.Native.Call(entryAddress, contextAddress, parameters, results)
}

func (i *FirmwareImageInvoker) Cancel(contextAddress uint32) TransportCode {
	if i == nil || i.Descriptor == nil {
		return TransportCodeState
	}
	d := i.Descriptor
	contextOffset, ok := d.rangeOffset(contextAddress, ContextABISize)
	if !ok {
		return TransportCodeState
	}
	cancelAddress := binary.LittleEndian.Uint32(d.Image[contextOffset+ContextCancelCellOffset:])
	cancelOffset, ok := d.rangeOffset(cancelAddress, 4)
	if !ok {
		return TransportCodeState
	}
	binary.LittleEndian.PutUint32(d.Image[cancelOffset:], 1)
	return TransportCodeOK
}

func (i *FirmwareImageInvoker) Reset(contextAddress uint32) TransportCode {
	if i == nil || i.Descriptor == nil || contextAddress != i.Descriptor.ContextAddress {
		return TransportCodeState
	}
	return i.restore()
}

func (i *FirmwareImageInvoker) clearTrap(contextAddress uint32) bool {
	d := i.Descriptor
	contextOffset, ok := d.rangeOffset(contextAddress, ContextABISize)
	if !ok {
		return false
	}
	trapAddress := binary.LittleEndian.Uint32(d.Image[contextOffset+ContextTrapCellOffset:])
	trapOffset, ok := d.rangeOffset(trapAddress, 4)
	if !ok {
		return false
	}
	binary.LittleEndian.PutUint32(d.Image[trapOffset:], 0)
	return true
}

func (i *FirmwareImageInvoker) restore() TransportCode {
	d := i.Descriptor
	if !d.valid() {
		return TransportCodeState
	}
	if len(d.InitialImageBytes) != 0 {
		copy(d.Image, d.InitialImageBytes)
	} else {
		copy(d.Image, d.InitialImage)
	}
	for n := 0; n < d.contextCount(); n++ {
		contextOffset, _ := d.rangeOffset(d.contextAt(n), ContextABISize)
		helperAddress := binary.LittleEndian.Uint32(d.Image[contextOffset+ContextHelperTableOffset:])
		helperOffset, _ := d.rangeOffset(helperAddress, HelperTableBytes)
		binary.LittleEndian.PutUint32(d.Image[helperOffset+HelperF64Offset:], d.HelperEntries[0])
		binary.LittleEndian.PutUint32(d.Image[helperOffset+HelperSIMDOffset:], d.HelperEntries[1])
		binary.LittleEndian.PutUint32(d.Image[helperOffset+HelperI64Offset:], d.HelperEntries[2])
		binary.LittleEndian.PutUint32(d.Image[helperOffset+HelperF32Offset:], d.HelperEntries[3])
		if d.StackLimit != 0 {
			binary.LittleEndian.PutUint32(d.Image[contextOffset+ContextStackLimitOffset:], d.StackLimit)
		}
	}
	if i.Publish != nil {
		i.Publish(d.ImageAddress, d.Image)
	}
	return TransportCodeOK
}

func (d *FirmwareImageDescriptor) valid() bool {
	if d == nil || (d.Target != TransportTargetArm32 && d.Target != TransportTargetRISCV32) ||
		d.MaximumPayload == 0 || d.ImageAddress == 0 || d.initialLength() == 0 ||
		(len(d.InitialImage) == 0) == (len(d.InitialImageBytes) == 0) ||
		d.initialLength() != len(d.Image) || uint64(len(d.Image)) > uint64(^uint32(0)) ||
		d.ContextAddress == 0 ||
		d.HelperEntries[0] == 0 || d.HelperEntries[1] == 0 ||
		d.HelperEntries[2] == 0 || d.HelperEntries[3] == 0 {
		return false
	}
	end := uint64(d.ImageAddress) + uint64(len(d.Image))
	if end > uint64(^uint32(0))+1 || !d.contextValid(d.ContextAddress) ||
		d.StartAddress != 0 && !d.callable(d.StartAddress) {
		return false
	}
	for n := 0; n < d.contextCount(); n++ {
		contextOffset, ok := d.rangeOffsetInitial(d.contextAt(n), ContextABISize)
		if !ok {
			return false
		}
		helperAddress := d.initialUint32(contextOffset + ContextHelperTableOffset)
		if _, ok := d.rangeOffsetInitial(helperAddress, HelperTableBytes); !ok {
			return false
		}
	}
	for _, function := range d.Functions {
		context := function.Context
		if context == 0 {
			context = d.ContextAddress
		}
		if function.Address == 0 || !d.callable(function.Address) || !d.contextValid(context) {
			return false
		}
	}
	return true
}

func (d *FirmwareImageDescriptor) contextCount() int {
	if len(d.Contexts) == 0 {
		return 1
	}
	return len(d.Contexts)
}

func (d *FirmwareImageDescriptor) contextAt(index int) uint32 {
	if len(d.Contexts) == 0 {
		return d.ContextAddress
	}
	return d.Contexts[index]
}

func (d *FirmwareImageDescriptor) contextValid(address uint32) bool {
	_, ok := d.rangeOffset(address, ContextABISize)
	return ok
}

func (d *FirmwareImageDescriptor) callable(callable uint32) bool {
	address := callable
	if d.Target != TransportTargetArm32 && d.Target != TransportTargetRISCV32 {
		return false
	}
	if d.Target == TransportTargetArm32 {
		if address&1 == 0 {
			return false
		}
		address &^= 1
	} else if address&1 != 0 {
		return false
	}
	_, ok := d.rangeOffset(address, 1)
	return ok
}

func (d *FirmwareImageDescriptor) rangeOffset(address, length uint32) (uint32, bool) {
	if uint64(len(d.Image)) > uint64(^uint32(0)) {
		return 0, false
	}
	return firmwareRangeOffset(d.ImageAddress, uint32(len(d.Image)), address, length)
}

func (d *FirmwareImageDescriptor) rangeOffsetInitial(address, length uint32) (uint32, bool) {
	if uint64(d.initialLength()) > uint64(^uint32(0)) {
		return 0, false
	}
	return firmwareRangeOffset(d.ImageAddress, uint32(d.initialLength()), address, length)
}

func (d *FirmwareImageDescriptor) initialLength() int {
	if len(d.InitialImageBytes) != 0 {
		return len(d.InitialImageBytes)
	}
	return len(d.InitialImage)
}

func (d *FirmwareImageDescriptor) initialUint32(offset uint32) uint32 {
	if len(d.InitialImageBytes) != 0 {
		return binary.LittleEndian.Uint32(d.InitialImageBytes[offset:])
	}
	return firmwareStringUint32(d.InitialImage, offset)
}

func firmwareStringUint32(value string, offset uint32) uint32 {
	return uint32(value[offset]) |
		uint32(value[offset+1])<<8 |
		uint32(value[offset+2])<<16 |
		uint32(value[offset+3])<<24
}

func firmwareRangeOffset(base, imageSize, address, length uint32) (uint32, bool) {
	if address < base {
		return 0, false
	}
	start := uint64(address)
	end := start + uint64(length)
	imageEnd := uint64(base) + uint64(imageSize)
	if end < start || end > imageEnd {
		return 0, false
	}
	return address - base, true
}
