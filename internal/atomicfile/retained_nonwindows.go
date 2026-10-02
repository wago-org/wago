//go:build !windows && !darwin && !linux

package atomicfile

import "os"

type retainedReplaceHandle struct{}

func createRetainedReplacementTemp(destination string, requireExistingParent bool) (*os.File, retainedReplaceHandle, error) {
	file, err := createTempWithParentPolicy(destination, requireExistingParent)
	return file, retainedReplaceHandle{}, err
}

func (retainedReplaceHandle) valid() bool          { return false }
func (retainedReplaceHandle) replace(string) error { return nil }
func (retainedReplaceHandle) remove() error        { return nil }
func (retainedReplaceHandle) close() error         { return nil }
