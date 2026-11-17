//go:build linux && amd64 && !tinygo

package amd64

import "golang.org/x/sys/cpu"

func workerResetModernCPU() bool {
	return cpu.X86.HasSSSE3 && cpu.X86.HasSSE41 && cpu.X86.HasSSE42 && cpu.X86.HasAVX
}
