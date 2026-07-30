//go:build !windows

package wagocli

import "os"

func replaceSelfExecutable(executable, staged string) (bool, error) {
	return false, os.Rename(staged, executable)
}

func removeSelfExecutable(executable string) (bool, error) {
	err := os.Remove(executable)
	if os.IsNotExist(err) {
		err = nil
	}
	return false, err
}
