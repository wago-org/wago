//go:build linux

package gocommand

func normalizeGroupSignalError(_ int, err error) error { return err }
