package wago

import (
	"math"
	"unsafe"
)

// Typed little-endian accessors over an instance's linear memory.
//
// They read/write through a single aligned machine load/store after one bounds
// check, which is faster than reaching for encoding/binary on Memory().Bytes() —
// and it closes the host-side gap under TinyGo, whose LLVM backend optimizes
// encoding/binary's per-byte assembly less aggressively (see docs/tinygo.md;
// ~0.43 ns/op vs ~1.6 ns/op for the binary idiom, at parity with the standard
// toolchain). wago targets little-endian amd64, so a native load already yields
// little-endian byte order.
//
// Each Read returns ok=false (and the zero value) when [offset, offset+size)
// falls outside linear memory; each Write returns false and writes nothing. This
// mirrors wazero's api.Memory bounds contract. Every public accessor also holds
// an invocation lease, so Instance.Close cannot recycle the backing JobMemory
// until an access that linearized first has completed.

// memPtr bounds-checks [offset, offset+size) against linear memory and returns a
// pointer to that location. size must be >= 1. Callers must hold an invocation
// lease or be executing inside an already-leased host callback.
func (in *Instance) memPtr(offset uint32, size int) (unsafe.Pointer, bool) {
	mem := in.mem()
	if uint64(offset)+uint64(size) > uint64(len(mem)) {
		return nil, false
	}
	// TinyGo lowers unsafe.Add with a uint32 offset through signed pointer
	// arithmetic on native targets, so addresses at or above 2 GiB can wrap into
	// unmapped space. Widen the already-bounds-checked offset before addition.
	return unsafe.Pointer(uintptr(unsafe.Pointer(&mem[0])) + uintptr(offset)), true
}

// mem returns the instance's LIVE linear memory. The JobMemory is the source of
// truth: after a memory.grow the base pointer stays put (the memory is a fixed
// full-size reservation) but the length grows, so a one-time cached slice would
// under-report size and reject host access to newly grown pages (e.g. a host
// fd_write of a guest buffer allocated after growth → EINVAL → guest panic).
// Callers must hold an invocation lease or be executing inside a host callback
// whose outer invocation owns one.
func (in *Instance) mem() []byte {
	if in.jm == nil {
		return nil
	}
	return in.jm.HostBytes()
}

func (in *Instance) readUint8NoLease(offset uint32) (uint8, bool) {
	p, ok := in.memPtr(offset, 1)
	if !ok {
		return 0, false
	}
	return *(*uint8)(p), true
}

func (in *Instance) readUint16LeNoLease(offset uint32) (uint16, bool) {
	p, ok := in.memPtr(offset, 2)
	if !ok {
		return 0, false
	}
	return *(*uint16)(p), true
}

func (in *Instance) readUint32LeNoLease(offset uint32) (uint32, bool) {
	p, ok := in.memPtr(offset, 4)
	if !ok {
		return 0, false
	}
	return *(*uint32)(p), true
}

func (in *Instance) readUint64LeNoLease(offset uint32) (uint64, bool) {
	p, ok := in.memPtr(offset, 8)
	if !ok {
		return 0, false
	}
	return *(*uint64)(p), true
}

func (in *Instance) writeUint8NoLease(offset uint32, v uint8) bool {
	p, ok := in.memPtr(offset, 1)
	if !ok {
		return false
	}
	*(*uint8)(p) = v
	return true
}

func (in *Instance) writeUint16LeNoLease(offset uint32, v uint16) bool {
	p, ok := in.memPtr(offset, 2)
	if !ok {
		return false
	}
	*(*uint16)(p) = v
	return true
}

func (in *Instance) writeUint32LeNoLease(offset uint32, v uint32) bool {
	p, ok := in.memPtr(offset, 4)
	if !ok {
		return false
	}
	*(*uint32)(p) = v
	return true
}

func (in *Instance) writeUint64LeNoLease(offset uint32, v uint64) bool {
	p, ok := in.memPtr(offset, 8)
	if !ok {
		return false
	}
	*(*uint64)(p) = v
	return true
}

// ReadUint8 returns the byte at offset. (Named Uint8, not Byte, so it does not
// collide with the io.ByteReader.ReadByte() (byte, error) contract that go vet
// enforces on any ReadByte method.)
func (in *Instance) ReadUint8(offset uint32) (uint8, bool) {
	if in == nil || in.beginInvocation() != nil {
		return 0, false
	}
	defer in.endInvocation()
	return in.readUint8NoLease(offset)
}

// ReadUint16Le returns the little-endian uint16 at offset.
func (in *Instance) ReadUint16Le(offset uint32) (uint16, bool) {
	if in == nil || in.beginInvocation() != nil {
		return 0, false
	}
	defer in.endInvocation()
	return in.readUint16LeNoLease(offset)
}

// ReadUint32Le returns the little-endian uint32 at offset.
func (in *Instance) ReadUint32Le(offset uint32) (uint32, bool) {
	if in == nil || in.beginInvocation() != nil {
		return 0, false
	}
	defer in.endInvocation()
	return in.readUint32LeNoLease(offset)
}

// ReadUint64Le returns the little-endian uint64 at offset.
func (in *Instance) ReadUint64Le(offset uint32) (uint64, bool) {
	if in == nil || in.beginInvocation() != nil {
		return 0, false
	}
	defer in.endInvocation()
	return in.readUint64LeNoLease(offset)
}

// ReadFloat32Le returns the little-endian float32 at offset.
func (in *Instance) ReadFloat32Le(offset uint32) (float32, bool) {
	if in == nil || in.beginInvocation() != nil {
		return 0, false
	}
	defer in.endInvocation()
	v, ok := in.readUint32LeNoLease(offset)
	return math.Float32frombits(v), ok
}

// ReadFloat64Le returns the little-endian float64 at offset.
func (in *Instance) ReadFloat64Le(offset uint32) (float64, bool) {
	if in == nil || in.beginInvocation() != nil {
		return 0, false
	}
	defer in.endInvocation()
	v, ok := in.readUint64LeNoLease(offset)
	return math.Float64frombits(v), ok
}

// WriteUint8 stores v at offset. (Named Uint8, not Byte, to avoid the
// io.ByteWriter.WriteByte(byte) error contract that go vet enforces.)
func (in *Instance) WriteUint8(offset uint32, v uint8) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	return in.writeUint8NoLease(offset, v)
}

// WriteUint16Le stores v at offset in little-endian order.
func (in *Instance) WriteUint16Le(offset uint32, v uint16) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	return in.writeUint16LeNoLease(offset, v)
}

// WriteUint32Le stores v at offset in little-endian order.
func (in *Instance) WriteUint32Le(offset uint32, v uint32) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	return in.writeUint32LeNoLease(offset, v)
}

// WriteUint64Le stores v at offset in little-endian order.
func (in *Instance) WriteUint64Le(offset uint32, v uint64) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	return in.writeUint64LeNoLease(offset, v)
}

// WriteFloat32Le stores v at offset in little-endian order.
func (in *Instance) WriteFloat32Le(offset uint32, v float32) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	return in.writeUint32LeNoLease(offset, math.Float32bits(v))
}

// WriteFloat64Le stores v at offset in little-endian order.
func (in *Instance) WriteFloat64Le(offset uint32, v float64) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	return in.writeUint64LeNoLease(offset, math.Float64bits(v))
}

// Read returns a copy of length bytes starting at offset, or ok=false if the
// range falls outside linear memory. For zero-copy access use Memory().Bytes().
func (in *Instance) Read(offset, length uint32) ([]byte, bool) {
	if in == nil || in.beginInvocation() != nil {
		return nil, false
	}
	defer in.endInvocation()
	mem := in.mem()
	if uint64(offset)+uint64(length) > uint64(len(mem)) {
		return nil, false
	}
	out := make([]byte, length)
	copy(out, mem[offset:offset+length])
	return out, true
}

// Write copies b into linear memory at offset, returning false (and writing
// nothing) if the range falls outside linear memory.
func (in *Instance) Write(offset uint32, b []byte) bool {
	if in == nil || in.beginInvocation() != nil {
		return false
	}
	defer in.endInvocation()
	mem := in.mem()
	if uint64(offset)+uint64(len(b)) > uint64(len(mem)) {
		return false
	}
	copy(mem[offset:], b)
	return true
}
