package qemu32

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const (
	CodeBase       = uint32(0x10000)
	ArmF32Helper   = uint32(0x18001)
	ArmF64Helper   = uint32(0x18101)
	ArmI64Helper   = uint32(0x18201)
	ArmSIMDHelper  = uint32(0x18301)
	RVF32Helper    = uint32(0x18000)
	RVF64Helper    = uint32(0x18100)
	RVI64Helper    = uint32(0x18200)
	RVSIMDHelper   = uint32(0x18300)
	ImageBase      = uint32(0x20000)
	maximumSlots   = uint32(256)
	requestWords   = uint32(5) + maximumSlots
	responseWords  = uint32(3) + maximumSlots
	protocolCall   = uint32(1)
	protocolStart  = uint32(2)
	protocolExit   = uint32(3)
	protocolRead   = uint32(4)
	protocolWrite  = uint32(5)
	protocolHelper = uint32(1)
	protocolResult = uint32(2)
)

func ArmHelpers() [4]uint32 {
	return [4]uint32{ArmF64Helper, ArmSIMDHelper, ArmI64Helper, ArmF32Helper}
}
func RVHelpers() [4]uint32 { return [4]uint32{RVF64Helper, RVSIMDHelper, RVI64Helper, RVF32Helper} }

type Layout struct {
	RequestAddress      uint32
	ResponseAddress     uint32
	CallAddress         uint32
	HelperHeaderAddress uint32
	HelperStateAddress  uint32
}

func DataLayout(imageBytes uint32) Layout {
	next := align16(ImageBase + imageBytes)
	layout := Layout{RequestAddress: next}
	next += requestWords * 4
	layout.ResponseAddress = next
	next += responseWords * 4
	layout.CallAddress = next
	next += embedded32.CallABIBytes
	layout.HelperHeaderAddress = next
	next += 12
	layout.HelperStateAddress = next
	return layout
}

func align16(value uint32) uint32 { return (value + 15) &^ 15 }

type Client struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
}

func Start(qemu, elf string) (*Client, error) {
	command := exec.Command(qemu, elf)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	return &Client{command: command, stdin: stdin, stdout: stdout}, nil
}

func (c *Client) StartFunction(entry, context uint32) (embedded32.Trap, error) {
	return c.request(protocolStart, entry, context, nil, 0, nil)
}

func (c *Client) Write(address uint32, data []byte) error {
	for len(data) != 0 {
		count := len(data)
		if count > 1024 {
			count = 1024
		}
		var header [20]byte
		binary.LittleEndian.PutUint32(header[0:4], protocolWrite)
		binary.LittleEndian.PutUint32(header[4:8], address)
		binary.LittleEndian.PutUint32(header[16:20], uint32(count))
		if _, err := c.stdin.Write(header[:]); err != nil {
			return err
		}
		if _, err := c.stdin.Write(data[:count]); err != nil {
			return err
		}
		var response [12]byte
		if _, err := io.ReadFull(c.stdout, response[:]); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(response[0:4]) != protocolResult || binary.LittleEndian.Uint32(response[4:8]) != 0 || binary.LittleEndian.Uint32(response[8:12]) != 0 {
			return fmt.Errorf("qemu32: invalid write response %x", response)
		}
		address += uint32(count)
		data = data[count:]
	}
	return nil
}

func (c *Client) Read(address, slots uint32) ([]uint32, error) {
	if slots > maximumSlots {
		return nil, fmt.Errorf("qemu32: read slots exceed %d", maximumSlots)
	}
	results := make([]uint32, slots)
	trap, err := c.request(protocolRead, address, 0, nil, slots, results)
	if err != nil {
		return nil, err
	}
	if trap != embedded32.TrapNone {
		return nil, fmt.Errorf("qemu32: read returned trap %d", trap)
	}
	return results, nil
}

func (c *Client) Call(entry, context uint32, parameters []uint32, resultSlots uint32) ([]uint32, embedded32.Trap, error) {
	if len(parameters) > int(maximumSlots) || resultSlots > maximumSlots {
		return nil, embedded32.TrapNone, fmt.Errorf("qemu32: call slots exceed %d", maximumSlots)
	}
	results := make([]uint32, resultSlots)
	trap, err := c.request(protocolCall, entry, context, parameters, resultSlots, results)
	return results, trap, err
}

func (c *Client) request(op, entry, context uint32, parameters []uint32, resultSlots uint32, results []uint32) (embedded32.Trap, error) {
	var header [20]byte
	binary.LittleEndian.PutUint32(header[0:4], op)
	binary.LittleEndian.PutUint32(header[4:8], entry)
	binary.LittleEndian.PutUint32(header[8:12], context)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(parameters)))
	binary.LittleEndian.PutUint32(header[16:20], resultSlots)
	if _, err := c.stdin.Write(header[:]); err != nil {
		return embedded32.TrapNone, err
	}
	if len(parameters) != 0 {
		payload := make([]byte, len(parameters)*4)
		for i, value := range parameters {
			binary.LittleEndian.PutUint32(payload[i*4:], value)
		}
		if _, err := c.stdin.Write(payload); err != nil {
			return embedded32.TrapNone, err
		}
	}
	for {
		var response [12]byte
		if _, err := io.ReadFull(c.stdout, response[:]); err != nil {
			return embedded32.TrapNone, err
		}
		switch binary.LittleEndian.Uint32(response[0:4]) {
		case protocolHelper:
			kind := binary.LittleEndian.Uint32(response[4:8])
			size := binary.LittleEndian.Uint32(response[8:12])
			frame := make([]byte, size)
			if _, err := io.ReadFull(c.stdout, frame); err != nil {
				return embedded32.TrapNone, err
			}
			if kind == 3 {
				if err := c.executeSIMDHelper(frame); err != nil {
					return embedded32.TrapNone, err
				}
			} else {
				if err := executeHelper(kind, frame); err != nil {
					return embedded32.TrapNone, err
				}
				if _, err := c.stdin.Write(frame); err != nil {
					return embedded32.TrapNone, err
				}
			}
		case protocolResult:
			code := embedded32.Trap(binary.LittleEndian.Uint32(response[4:8]))
			count := binary.LittleEndian.Uint32(response[8:12])
			if count > maximumSlots {
				return embedded32.TrapNone, fmt.Errorf("qemu32: invalid result count %d", count)
			}
			payload := make([]byte, count*4)
			if _, err := io.ReadFull(c.stdout, payload); err != nil {
				return embedded32.TrapNone, err
			}
			if code == embedded32.TrapNone {
				if uint32(len(results)) != count {
					return embedded32.TrapNone, fmt.Errorf("qemu32: result count %d, want %d", count, len(results))
				}
				for i := range results {
					results[i] = binary.LittleEndian.Uint32(payload[i*4:])
				}
			} else if count != 0 {
				return embedded32.TrapNone, errors.New("qemu32: trapped result carried payload")
			}
			return code, nil
		default:
			return embedded32.TrapNone, fmt.Errorf("qemu32: unknown response tag %d", binary.LittleEndian.Uint32(response[0:4]))
		}
	}
}

func (c *Client) Close() error {
	if c == nil || c.command == nil {
		return nil
	}
	var request [20]byte
	binary.LittleEndian.PutUint32(request[0:4], protocolExit)
	_, _ = c.stdin.Write(request[:])
	_ = c.stdin.Close()
	err := c.command.Wait()
	_ = c.stdout.Close()
	return err
}

func executeHelper(kind uint32, frame []byte) error {
	if len(frame) == 0 {
		return errors.New("qemu32: empty helper frame")
	}
	switch kind {
	case 0:
		if len(frame) != int(embedded32.F32FrameBytes) {
			return fmt.Errorf("qemu32: f32 frame bytes=%d", len(frame))
		}
		embedded32.RunF32((*embedded32.F32Frame)(unsafe.Pointer(&frame[0])))
	case 1:
		if len(frame) != int(embedded32.F64FrameBytes) {
			return fmt.Errorf("qemu32: f64 frame bytes=%d", len(frame))
		}
		embedded32.RunF64((*embedded32.F64Frame)(unsafe.Pointer(&frame[0])))
	case 2:
		if len(frame) != int(embedded32.I64FrameBytes) {
			return fmt.Errorf("qemu32: i64 frame bytes=%d", len(frame))
		}
		embedded32.RunI64((*embedded32.I64Frame)(unsafe.Pointer(&frame[0])))
	default:
		return fmt.Errorf("qemu32: unknown helper kind %d", kind)
	}
	return nil
}

func (c *Client) executeSIMDHelper(frame []byte) error {
	if len(frame) != int(embedded32.SIMDFrameBytes) {
		return fmt.Errorf("qemu32: simd frame bytes=%d", len(frame))
	}
	get := func(offset uint32) uint32 { return binary.LittleEndian.Uint32(frame[offset : offset+4]) }
	f := embedded32.SIMDFrame{
		Op:      get(embedded32.SIMDFrameOpOffset),
		Scalar:  uint64(get(embedded32.SIMDFrameScalarLoOffset)) | uint64(get(embedded32.SIMDFrameScalarHiOffset))<<32,
		Address: get(embedded32.SIMDFrameAddressOffset),
		Lane:    get(embedded32.SIMDFrameLaneOffset),
	}
	copy(f.A[:], frame[embedded32.SIMDFrameAOffset:embedded32.SIMDFrameAOffset+16])
	copy(f.B[:], frame[embedded32.SIMDFrameBOffset:embedded32.SIMDFrameBOffset+16])
	copy(f.C[:], frame[embedded32.SIMDFrameCOffset:embedded32.SIMDFrameCOffset+16])
	copy(f.Immediate[:], frame[embedded32.SIMDFrameImmediateOffset:embedded32.SIMDFrameImmediateOffset+16])
	memoryBase := get(embedded32.SIMDFrameMemoryBaseOffset)
	memoryLength := get(embedded32.SIMDFrameMemoryLenOffset)
	width, store := simdMemoryAccess(f.Op)
	var request [12]byte
	var memory []byte
	if width != 0 && uint64(f.Address)+uint64(width) <= uint64(memoryLength) && uint64(memoryBase)+uint64(f.Address) <= uint64(^uint32(0)) {
		binary.LittleEndian.PutUint32(request[0:4], memoryBase+f.Address)
		binary.LittleEndian.PutUint32(request[4:8], width)
		if store {
			binary.LittleEndian.PutUint32(request[8:12], width)
		}
		memory = make([]byte, width)
		f.Memory = memory
		f.Address = 0
	}
	if _, err := c.stdin.Write(request[:]); err != nil {
		return err
	}
	if len(memory) != 0 {
		if _, err := io.ReadFull(c.stdout, memory); err != nil {
			return err
		}
	}
	embedded32.RunSIMD(&f)
	copy(frame[embedded32.SIMDFrameOutOffset:embedded32.SIMDFrameOutOffset+16], f.Out[:])
	binary.LittleEndian.PutUint32(frame[embedded32.SIMDFrameScalarOutOffset:embedded32.SIMDFrameScalarOutOffset+4], uint32(f.ScalarOut))
	binary.LittleEndian.PutUint32(frame[embedded32.SIMDFrameScalarOutOffset+4:embedded32.SIMDFrameScalarOutOffset+8], uint32(f.ScalarOut>>32))
	binary.LittleEndian.PutUint32(frame[embedded32.SIMDFrameTrapOffset:embedded32.SIMDFrameTrapOffset+4], uint32(f.Trap))
	if _, err := c.stdin.Write(frame); err != nil {
		return err
	}
	if store && len(memory) != 0 {
		if _, err := c.stdin.Write(memory); err != nil {
			return err
		}
	}
	return nil
}

func simdMemoryAccess(op uint32) (width uint32, store bool) {
	switch op {
	case 0, 11:
		return 16, op == 11
	case 1, 2, 3, 4, 5, 6:
		return 8, false
	case 7, 84, 88:
		return 1, op == 88
	case 8, 85, 89:
		return 2, op == 89
	case 9, 86, 90, 92:
		return 4, op == 90
	case 10, 87, 91, 93:
		return 8, op == 91
	default:
		return 0, false
	}
}
