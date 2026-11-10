//go:build windows

package managedrelease

import (
	"errors"
	"syscall"
	"testing"
)

func TestRetryWindowsFilesystemOperationRetriesSharingViolations(t *testing.T) {
	attempts := 0
	err := retryWindowsFilesystemOperation(func() error {
		attempts++
		if attempts < 3 {
			return syscall.ERROR_ACCESS_DENIED
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("retry result: attempts=%d, err=%v", attempts, err)
	}

	want := errors.New("permanent failure")
	attempts = 0
	err = retryWindowsFilesystemOperation(func() error {
		attempts++
		return want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("non-retryable result: attempts=%d, err=%v", attempts, err)
	}
}
