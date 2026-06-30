package wago

import (
	"math"
	"unsafe"
)

// Typed little-endian accessors over an instance's linear memory.
//
// They read/write through a single aligned machine load/store after one bounds
// check, which is faster than reaching for encoding/binary on LinearMemory() —
// and it closes the host-side gap under TinyGo, whose LLVM backend optimizes
// encoding/binary's per-byte assembly less aggressively (see docs/tinygo.md;
// ~0.43 ns/op vs ~1.6 ns/op for the binary idiom, at parity with the standard
// toolchain). wago targets little-endian amd64, so a native load already yields
// little-endian byte order.
//
// Each Read returns ok=false (and the zero value) when [offset, offset+size)
// falls outside linear memory; each Write returns false and writes nothing. This
// mirrors wazero's api.Memory bounds contract.

// memPtr bounds-checks [offset, offset+size) against linear memory and returns a
// pointer to that location. size must be >= 1.
func (in *Instance) memPtr(offset uint32, size int) (unsafe.Pointer, bool) {
	if uint64(offset)+uint64(size) > uint64(len(in.linMem)) {
		return nil, false
	}
	return unsafe.Add(unsafe.Pointer(&in.linMem[0]), offset), true
}

// ReadByte returns the byte at offset.
func (in *Instance) ReadByte(offset uint32) (byte, bool) {
	p, ok := in.memPtr(offset, 1)
	if !ok {
		return 0, false
	}
	return *(*byte)(p), true
}

// ReadUint16Le returns the little-endian uint16 at offset.
func (in *Instance) ReadUint16Le(offset uint32) (uint16, bool) {
	p, ok := in.memPtr(offset, 2)
	if !ok {
		return 0, false
	}
	return *(*uint16)(p), true
}

// ReadUint32Le returns the little-endian uint32 at offset.
func (in *Instance) ReadUint32Le(offset uint32) (uint32, bool) {
	p, ok := in.memPtr(offset, 4)
	if !ok {
		return 0, false
	}
	return *(*uint32)(p), true
}

// ReadUint64Le returns the little-endian uint64 at offset.
func (in *Instance) ReadUint64Le(offset uint32) (uint64, bool) {
	p, ok := in.memPtr(offset, 8)
	if !ok {
		return 0, false
	}
	return *(*uint64)(p), true
}

// ReadFloat32Le returns the little-endian float32 at offset.
func (in *Instance) ReadFloat32Le(offset uint32) (float32, bool) {
	v, ok := in.ReadUint32Le(offset)
	return math.Float32frombits(v), ok
}

// ReadFloat64Le returns the little-endian float64 at offset.
func (in *Instance) ReadFloat64Le(offset uint32) (float64, bool) {
	v, ok := in.ReadUint64Le(offset)
	return math.Float64frombits(v), ok
}

// WriteByte stores v at offset.
func (in *Instance) WriteByte(offset uint32, v byte) bool {
	p, ok := in.memPtr(offset, 1)
	if !ok {
		return false
	}
	*(*byte)(p) = v
	return true
}

// WriteUint16Le stores v at offset in little-endian order.
func (in *Instance) WriteUint16Le(offset uint32, v uint16) bool {
	p, ok := in.memPtr(offset, 2)
	if !ok {
		return false
	}
	*(*uint16)(p) = v
	return true
}

// WriteUint32Le stores v at offset in little-endian order.
func (in *Instance) WriteUint32Le(offset uint32, v uint32) bool {
	p, ok := in.memPtr(offset, 4)
	if !ok {
		return false
	}
	*(*uint32)(p) = v
	return true
}

// WriteUint64Le stores v at offset in little-endian order.
func (in *Instance) WriteUint64Le(offset uint32, v uint64) bool {
	p, ok := in.memPtr(offset, 8)
	if !ok {
		return false
	}
	*(*uint64)(p) = v
	return true
}

// WriteFloat32Le stores v at offset in little-endian order.
func (in *Instance) WriteFloat32Le(offset uint32, v float32) bool {
	return in.WriteUint32Le(offset, math.Float32bits(v))
}

// WriteFloat64Le stores v at offset in little-endian order.
func (in *Instance) WriteFloat64Le(offset uint32, v float64) bool {
	return in.WriteUint64Le(offset, math.Float64bits(v))
}

// Read returns a copy of length bytes starting at offset, or ok=false if the
// range falls outside linear memory. For zero-copy access use LinearMemory().
func (in *Instance) Read(offset, length uint32) ([]byte, bool) {
	if uint64(offset)+uint64(length) > uint64(len(in.linMem)) {
		return nil, false
	}
	out := make([]byte, length)
	copy(out, in.linMem[offset:offset+length])
	return out, true
}

// Write copies b into linear memory at offset, returning false (and writing
// nothing) if the range falls outside linear memory.
func (in *Instance) Write(offset uint32, b []byte) bool {
	if uint64(offset)+uint64(len(b)) > uint64(len(in.linMem)) {
		return false
	}
	copy(in.linMem[offset:], b)
	return true
}
