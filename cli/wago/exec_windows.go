//go:build windows

package main

import (
	"os"
	"os/exec"
)

// execProcess runs bin to completion and exits with its status (Windows has no
// exec-replace), so `wago run` still behaves as a single foreground command.
func execProcess(bin string, args, env []string) error {
	cmd := exec.Command(bin, args[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
