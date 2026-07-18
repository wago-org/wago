//go:build !wago_guardpage || (!linux && !darwin) || (linux && !amd64 && !arm64 && !riscv64) || (darwin && !arm64)

package wagobench

func corpusGuardEnabled() bool { return false }
