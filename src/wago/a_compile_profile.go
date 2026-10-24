package wago

import (
	"github.com/wago-org/wago/internal/jitprofile"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"unsafe"
)

// unmapProfileCode is called only after executable ownership is released. Retire
// before releasing the address to the OS so a concurrent address reuse cannot
// overtake the old generation's retirement. No guest can execute in this interval.
func (c *Compiled) unmapProfileCode(mem []byte) error {
	if len(mem) == 0 {
		return nil
	}
	if cc := c.loadCodeCache(); jitprofile.Enabled && cc != nil {
		cc.retireProfileImage(uintptr(unsafe.Pointer(&mem[0])))
	}
	if indexes := c.loadCompileIndexes(); indexes != nil && indexes.unmapCode != nil {
		return indexes.unmapCode(mem)
	}
	return coreruntime.Unmap(mem)
}
