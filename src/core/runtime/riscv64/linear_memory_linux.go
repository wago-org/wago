//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// This experimental cap bounds reserved virtual address space; pages are
// anonymous zeroed RW memory, never executable. No growth moves a guest base.
const MemoryPageBytes = 65536
const MaxMemoryPages = 256

// LinearMemory owns one unshared wasm32 memory and a stable three-cell
// descriptor: base, logical byte length, effective maximum byte length.
// Host methods and leased guest calls serialize on the same lock.
type LinearMemory struct {
	mu              sync.Mutex
	mem, descriptor []byte
	min, max        uint32
	declaredMax     uint64
	hasMax          bool
	refs            int
	closed          bool
	ops             *memoryOps
}

// NewLinearMemory creates an owner with an explicit declared maximum. A nonnil
// closed owner returned with an error retains failed cleanup; Close retries it.
func NewLinearMemory(min, max uint32) (*LinearMemory, error) {
	return newLinearMemory(min, max, &linuxMemory)
}

// NewLinearMemoryWithLimits preserves the Wasm declared-maximum provenance
// independently of the reservation quota. On error, a nonnil closed owner must
// be closed again to retry retained cleanup.
func NewLinearMemoryWithLimits(min uint32, max uint64, hasMax bool) (*LinearMemory, error) {
	return newLinearMemoryLimits(min, max, hasMax, &linuxMemory)
}
func newLinearMemory(min, max uint32, ops *memoryOps) (*LinearMemory, error) {
	return newLinearMemoryLimits(min, uint64(max), true, ops)
}
func newLinearMemoryLimits(min uint32, declaredMax uint64, hasMax bool, ops *memoryOps) (*LinearMemory, error) {
	max := uint32(MaxMemoryPages)
	if hasMax {
		if declaredMax > 65536 {
			return nil, fmt.Errorf("rv64: invalid wasm32 maximum")
		}
		if declaredMax < uint64(max) {
			max = uint32(declaredMax)
		}
	}
	if min > max {
		return nil, fmt.Errorf("rv64: memory limits outside 0..%d pages", MaxMemoryPages)
	}
	m := &LinearMemory{min: min, max: max, declaredMax: declaredMax, hasMax: hasMax, refs: 1, ops: ops}
	var err error
	if max != 0 {
		m.mem, err = ops.mapRW(int(max) * MemoryPageBytes)
	}
	if err == nil {
		m.descriptor, err = ops.mapRW(24)
	}
	if err != nil {
		m.closed = true
		m.refs = 0
		if cleanup := m.retire(); cleanup != nil {
			return m, errors.Join(err, cleanup)
		}
		return nil, err
	}
	binary.LittleEndian.PutUint64(m.descriptor, uint64(pointer(m.mem)))
	binary.LittleEndian.PutUint64(m.descriptor[8:], uint64(min)*MemoryPageBytes)
	binary.LittleEndian.PutUint64(m.descriptor[16:], uint64(max)*MemoryPageBytes)
	return m, nil
}
func (m *LinearMemory) length() uint64 { return binary.LittleEndian.Uint64(m.descriptor[8:]) }
func memoryRange(length uint64, off uint64, n int) bool {
	return n >= 0 && uint64(n) <= length && off <= length-uint64(n)
}
func (m *LinearMemory) Read(off uint32, dst []byte) error {
	if m == nil {
		return ErrClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	if !memoryRange(m.length(), uint64(off), len(dst)) {
		return fmt.Errorf("rv64: memory read out of bounds")
	}
	copy(dst, m.mem[int(off):int(off)+len(dst)])
	return nil
}
func (m *LinearMemory) Write(off uint32, src []byte) error {
	if m == nil {
		return ErrClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	if !memoryRange(m.length(), uint64(off), len(src)) {
		return fmt.Errorf("rv64: memory write out of bounds")
	}
	copy(m.mem[int(off):int(off)+len(src)], src)
	return nil
}
func (m *LinearMemory) Pages() (uint32, error) {
	if m == nil {
		return 0, ErrClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.descriptor) < 24 {
		return 0, ErrClosed
	}
	return uint32(m.length() / MemoryPageBytes), nil
}

// Grow returns the old size or false without changing memory. The reservation
// beyond logical length is untouched until first growth, so newly exposed pages
// are zero without a guest callback, relocation or per-call allocation.
func (m *LinearMemory) Grow(delta uint32) (uint32, bool) {
	if m == nil {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.descriptor) < 24 {
		return 0, false
	}
	old := uint32(m.length() / MemoryPageBytes)
	if delta > m.max-old {
		return old, false
	}
	binary.LittleEndian.PutUint64(m.descriptor[8:], uint64(old+delta)*MemoryPageBytes)
	return old, true
}

// Lease retains memory beyond creator Close. New leases then fail; existing
// instances remain valid until their own lease closes. Each lease is once-owned.
type MemoryLease struct {
	memory *LinearMemory
	token  *memoryLeaseToken
}
type memoryLeaseToken struct{ closed bool }

func (l *MemoryLease) Read(off uint32, dst []byte) error {
	if l == nil || l.memory == nil || l.token == nil {
		return ErrClosed
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.token.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	if !memoryRange(m.length(), uint64(off), len(dst)) {
		return fmt.Errorf("rv64: leased memory read out of bounds")
	}
	copy(dst, m.mem[int(off):int(off)+len(dst)])
	return nil
}
func (l *MemoryLease) Write(off uint32, src []byte) error {
	if l == nil || l.memory == nil || l.token == nil {
		return ErrClosed
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.token.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	if !memoryRange(m.length(), uint64(off), len(src)) {
		return fmt.Errorf("rv64: leased memory write out of bounds")
	}
	copy(m.mem[int(off):int(off)+len(src)], src)
	return nil
}

func (m *LinearMemory) Lease() (*MemoryLease, error) {
	return m.LeaseFor(0, 0, false)
}

// LeaseFor acquires and type-checks an import atomically. A reservation quota
// does not establish a declared maximum for Wasm import matching.
func (m *LinearMemory) LeaseFor(min uint32, max uint64, hasMax bool) (*MemoryLease, error) {
	if m == nil {
		return nil, ErrClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.descriptor) < 24 {
		return nil, ErrClosed
	}
	if uint64(m.length()/MemoryPageBytes) < uint64(min) || hasMax && (!m.hasMax || m.declaredMax > max) {
		return nil, fmt.Errorf("rv64: imported memory limits do not match")
	}
	m.refs++
	return &MemoryLease{memory: m, token: &memoryLeaseToken{}}, nil
}
func (l *MemoryLease) Descriptor() (uint64, error) {
	if l == nil || l.memory == nil || l.token == nil {
		return 0, ErrClosed
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.token.closed || len(m.descriptor) < 24 {
		return 0, ErrClosed
	}
	return uint64(pointer(m.descriptor)), nil
}
func (l *MemoryLease) Matches(min uint32, max uint64, hasMax bool) error {
	if l == nil || l.memory == nil || l.token == nil {
		return ErrClosed
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.token.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	if uint64(m.length()/MemoryPageBytes) < uint64(min) || hasMax && (!m.hasMax || m.declaredMax > max) {
		return fmt.Errorf("rv64: imported memory limits do not match")
	}
	return nil
}

type MemoryInitializer struct {
	Offset uint32
	Bytes  []byte
}

func (l *MemoryLease) Initialize(segments []MemoryInitializer) error {
	if l == nil || l.memory == nil || l.token == nil {
		return ErrClosed
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.token.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	for _, s := range segments {
		if !memoryRange(m.length(), uint64(s.Offset), len(s.Bytes)) {
			return fmt.Errorf("rv64: active data out of bounds")
		}
		copy(m.mem[int(s.Offset):int(s.Offset)+len(s.Bytes)], s.Bytes)
	}
	return nil
}
func (l *MemoryLease) callInto(f *CallFrame, e *Engine, p *Program, offset int, args, results []uint64, frame int, context, descriptor uint64) error {
	if l == nil || l.memory == nil || l.token == nil {
		return ErrClosed
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.token.closed || len(m.descriptor) < 24 {
		return ErrClosed
	}
	if descriptor != uint64(pointer(m.descriptor)) {
		return fmt.Errorf("rv64: context memory lease mismatch")
	}
	return f.CallIntoWithContext(e, p, offset, args, results, frame, context)
}
func (l *MemoryLease) Close() error {
	if l == nil || l.memory == nil || l.token == nil {
		return nil
	}
	m := l.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if !l.token.closed {
		l.token.closed = true
		m.refs--
	}
	if m.refs == 0 {
		return m.retire()
	}
	return nil
}
func (m *LinearMemory) retire() error {
	var a, b error
	if len(m.mem) != 0 {
		a = m.ops.unmap(m.mem)
		if a == nil {
			m.mem = nil
		}
	}
	if len(m.descriptor) != 0 {
		b = m.ops.unmap(m.descriptor)
		if b == nil {
			m.descriptor = nil
		}
	}
	return errors.Join(a, b)
}
func (m *LinearMemory) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		if m.refs > 0 {
			m.refs--
		}
	}
	if m.refs == 0 {
		return m.retire()
	}
	return nil
}

// GuestContext owns a stable off-heap memory-descriptor cell, integer globals,
// data-segment descriptors and copied passive bytes in one RW mapping. Its lock
// covers guest entry, global access and Close. No heap pointer is passed to
// generated code. The compiler owns and validates cell interpretation.
type GuestContext struct {
	mu     sync.Mutex
	mem    []byte
	n      int
	closed bool
	ops    *memoryOps
}

func NewGuestContext(memoryDescriptor uint64, globals []uint64) (*GuestContext, error) {
	return NewGuestContextWithData(memoryDescriptor, globals, nil)
}

const MaxDataSegments = 256
const MaxGuestDataBytes = 1 << 20

// NewGuestContextWithData appends (base,length) descriptors in module data-index
// order, then owned passive bytes. Active/dropped entries are supplied as nil.
// Empty entries never need a base. Dropping clears a descriptor length; storage
// stays owned until Close, bounding resources without guest callbacks.
func NewGuestContextWithData(memoryDescriptor uint64, globals []uint64, data [][]byte) (*GuestContext, error) {
	return newGuestContextWithData(memoryDescriptor, globals, data, &linuxMemory)
}
func newGuestContextWithData(memoryDescriptor uint64, globals []uint64, data [][]byte, ops *memoryOps) (*GuestContext, error) {
	if len(globals) > 256 {
		return nil, fmt.Errorf("rv64: too many scalar globals")
	}
	if len(data) > MaxDataSegments {
		return nil, fmt.Errorf("rv64: too many data segments")
	}
	bytes := 0
	for _, b := range data {
		if len(b) > MaxGuestDataBytes-bytes {
			return nil, fmt.Errorf("rv64: passive data exceeds context bound")
		}
		bytes += len(b)
	}
	dataOffset := (len(globals) + 1) * 8
	payload := dataOffset + len(data)*16
	mem, err := ops.mapRW(payload + bytes)
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint64(mem, memoryDescriptor)
	for j, v := range globals {
		binary.LittleEndian.PutUint64(mem[(j+1)*8:], v)
	}
	for j, b := range data {
		if len(b) == 0 {
			continue
		}
		desc := dataOffset + j*16
		binary.LittleEndian.PutUint64(mem[desc:], uint64(pointer(mem[payload:])))
		binary.LittleEndian.PutUint64(mem[desc+8:], uint64(len(b)))
		copy(mem[payload:], b)
		payload += len(b)
	}
	return &GuestContext{mem: mem, n: len(globals), ops: ops}, nil
}
func (c *GuestContext) Global(index int) (uint64, error) {
	if c == nil {
		return 0, ErrClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || index < 0 || index >= c.n {
		return 0, ErrClosed
	}
	return binary.LittleEndian.Uint64(c.mem[(index+1)*8:]), nil
}
func (c *GuestContext) Call(l *MemoryLease, f *CallFrame, e *Engine, p *Program, offset int, args []uint64, frame int) (uint64, error) {
	var result [1]uint64
	err := c.CallInto(l, f, e, p, offset, args, result[:], frame)
	return result[0], err
}
func (c *GuestContext) CallInto(l *MemoryLease, f *CallFrame, e *Engine, p *Program, offset int, args, results []uint64, frame int) error {
	if c == nil {
		return ErrClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || len(c.mem) == 0 {
		return ErrClosed
	}
	context := uint64(pointer(c.mem))
	if l != nil {
		return l.callInto(f, e, p, offset, args, results, frame, context, binary.LittleEndian.Uint64(c.mem))
	}
	if binary.LittleEndian.Uint64(c.mem) != 0 {
		return fmt.Errorf("rv64: context memory requires a lease")
	}
	return f.CallIntoWithContext(e, p, offset, args, results, frame, context)
}
func (c *GuestContext) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if len(c.mem) == 0 {
		return nil
	}
	err := c.ops.unmap(c.mem)
	if err == nil {
		c.mem = nil
	}
	return err
}
