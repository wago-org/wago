//go:build linux && !riscv64 && !tinygo

package riscv64

func nativeTailExitPointer() (uintptr, error) {
	return 0, unsupported("native tail execution requires Linux/RV64")
}
