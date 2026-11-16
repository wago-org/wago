package compactgc

import "unsafe"

// MemoryUsage accounts for backing capacities and the collector struct, not Go
// allocator size classes, runtime bookkeeping or RSS. Descriptor registration
// may temporarily allocate validation copies that are not retained here.
type MemoryUsage struct {
	Arena, Starts, Dirty, Marks, Queue, Remembered uint64
	Collector, Descriptors, ReferenceOffsets       uint64
}

func (m MemoryUsage) Total() uint64 {
	return m.Arena + m.Starts + m.Dirty + m.Marks + m.Queue + m.Remembered + m.Collector + m.Descriptors + m.ReferenceOffsets
}
func (c *Collector) MemoryUsage() MemoryUsage {
	m := MemoryUsage{Arena: uint64(len(c.arena)) + 15, Starts: uint64(cap(c.starts)), Dirty: uint64(cap(c.dirty)), Marks: uint64(cap(c.mark)) * 8, Queue: uint64(cap(c.queue)) * 4, Remembered: uint64(cap(c.remembered)) * 4, Collector: uint64(unsafe.Sizeof(*c)), Descriptors: uint64(cap(c.types)) * uint64(unsafe.Sizeof(Descriptor{}))}
	for _, d := range c.types {
		m.ReferenceOffsets += uint64(cap(d.RefOffsets)) * 4
	}
	return m
}
