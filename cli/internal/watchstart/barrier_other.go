//go:build !darwin

package watchstart

import "os/exec"

func Prepare(*exec.Cmd) error { return nil }

func Started(*exec.Cmd) error { return nil }

func Release(*exec.Cmd) error { return nil }

func Abort(*exec.Cmd) {}
