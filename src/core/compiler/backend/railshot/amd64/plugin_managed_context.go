//go:build amd64

package amd64

import x86 "github.com/wago-org/wago/src/core/encoder/amd64"

// managedPluginAMD64Context has the same storage as pluginAMD64Context but a
// deliberately smaller dynamic method set. Pointer conversion adds no wrapper
// allocation while preventing managed callbacks from asserting the raw Context.
type managedPluginAMD64Context pluginAMD64Context

func (c *managedPluginAMD64Context) full() *pluginAMD64Context {
	return (*pluginAMD64Context)(c)
}
func (c *managedPluginAMD64Context) InputI32(i int) (x86.Reg, error) { return c.full().InputI32(i) }
func (c *managedPluginAMD64Context) InputCustom(i int) ([]x86.Reg, error) {
	return c.full().InputCustom(i)
}
func (c *managedPluginAMD64Context) Release(r x86.Reg)       { c.full().Release(r) }
func (c *managedPluginAMD64Context) ReleaseGP(r x86.Reg)     { c.full().ReleaseGP(r) }
func (c *managedPluginAMD64Context) ReleaseVector(r x86.Reg) { c.full().ReleaseVector(r) }
func (c *managedPluginAMD64Context) ConstYMMRepeated128(lo, hi uint64) x86.Reg {
	return c.full().ConstYMMRepeated128(lo, hi)
}
func (c *managedPluginAMD64Context) LoadYMM(i int, off uint32) (x86.Reg, error) {
	return c.full().LoadYMM(i, off)
}
func (c *managedPluginAMD64Context) StoreYMM(i int, off uint32, r x86.Reg) error {
	return c.full().StoreYMM(i, off, r)
}
func (c *managedPluginAMD64Context) LoadZMM(i int, off uint32) (x86.Reg, error) {
	return c.full().LoadZMM(i, off)
}
func (c *managedPluginAMD64Context) StoreZMM(i int, off uint32, r x86.Reg) error {
	return c.full().StoreZMM(i, off, r)
}
func (c *managedPluginAMD64Context) OutputI32(r x86.Reg) error { return c.full().OutputI32(r) }
func (c *managedPluginAMD64Context) OutputCustom(regs ...x86.Reg) error {
	return c.full().OutputCustom(regs...)
}
