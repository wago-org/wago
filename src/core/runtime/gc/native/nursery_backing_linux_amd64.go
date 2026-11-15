//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package gc

import "syscall"

func (c *Collector) initOwnedNursery(size uint32) bool {
	mem, err := syscall.Mmap(-1, 0, int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return false
	}
	c.nursery = mem
	c.throughput.nurseryMapped = true
	return true
}

func (c *Collector) releaseOwnedNursery() {
	if !c.throughput.nurseryMapped {
		return
	}
	if err := syscall.Munmap(c.nursery); err != nil {
		// Retain the owner and extent so another Close can retry.
		panic("gc: cannot release nursery backing: " + err.Error())
	}
	c.throughput.nurseryMapped = false
}
