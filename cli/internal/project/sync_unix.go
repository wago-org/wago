//go:build !windows

package project

import (
	"errors"
	"os"
)

func syncProjectDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return joinClose(directory.Sync(), directory)
}

func joinClose(primary error, file *os.File) error {
	return errors.Join(primary, file.Close())
}
