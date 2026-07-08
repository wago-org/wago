//go:build !windows

package main

import "syscall"

// execProcess replaces the current process with bin (Unix exec), so the custom
// plugin binary takes over cleanly with no lingering parent.
func execProcess(bin string, args, env []string) error {
	return syscall.Exec(bin, args, env)
}
