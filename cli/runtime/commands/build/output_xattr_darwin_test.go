//go:build darwin

package build

import "golang.org/x/sys/unix"

const testUnpreservedXattrName = "com.wago.test"

func setTestUnpreservedXattr(path string, value []byte) error {
	return unix.Setxattr(path, testUnpreservedXattrName, value, 0)
}

func getTestUnpreservedXattr(path string) ([]byte, error) {
	value := make([]byte, 64)
	size, err := unix.Getxattr(path, testUnpreservedXattrName, value)
	if err != nil {
		return nil, err
	}
	return value[:size], nil
}
