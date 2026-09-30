//go:build !windows

package replace

import "os"

func StageRemoval(executable string, _ []string) (string, error) {
	return executable, nil
}

func Executable(executable, staged string) (bool, error) {
	return false, os.Rename(staged, executable)
}

func Remove(executable string) (bool, error) {
	err := os.Remove(executable)
	if os.IsNotExist(err) {
		err = nil
	}
	return false, err
}

func ScheduleTargetRemoval(_ string, _ []string, _ string, _ []string) (bool, error) {
	return false, nil
}
