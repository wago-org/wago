//go:build linux && !tinygo

package riscv64

func swarMemoryAlignment(size int) byte {
	switch size {
	case 1:
		return 0
	case 2:
		return 1
	case 4:
		return 2
	case 8:
		return 3
	default:
		return 0xff
	}
}
