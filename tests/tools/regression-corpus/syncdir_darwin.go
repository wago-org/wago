//go:build darwin

package main

import (
	"errors"
	"os"
	"syscall"
)

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	} else {
		return err
	}
}
