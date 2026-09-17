//go:build darwin

package compiler

import "syscall"

func hostCPUBrand() string {
	brand, err := syscall.Sysctl("machdep.cpu.brand_string")
	if err != nil {
		return ""
	}
	return brand
}
