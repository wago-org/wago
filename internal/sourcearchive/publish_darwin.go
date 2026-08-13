//go:build darwin

package sourcearchive

import "golang.org/x/sys/unix"

func publishDirectoryNoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
