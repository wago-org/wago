//go:build amd64 && tinygo && wago_lean

package amd64

// The lean TinyGo runtime keeps checked helper copies for dynamic lengths.
// Other profiles retain the inline overlap-aware numeric copy kernels.
func (f *fn) emitDynamicGCArrayCopy(uint32, uint32) bool { return false }
