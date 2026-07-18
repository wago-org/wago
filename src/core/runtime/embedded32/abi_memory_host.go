//go:build !arm && !riscv32

package embedded32

func memoryFromABI(base, size uint32) []byte {
	if base != 0 || size != 0 {
		panic("embedded32: 32-bit memory ABI used on a non-32-bit host")
	}
	return nil
}
