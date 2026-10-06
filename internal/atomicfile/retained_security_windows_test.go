//go:build windows

package atomicfile

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRetainedTempSecurityDACLDoesNotDuplicateLocalSystem(t *testing.T) {
	const want = "O:S-1-5-18D:P(A;;GA;;;S-1-5-18)"
	if got := retainedTempSecuritySDDL(localSystemSID); got != want {
		t.Fatalf("LocalSystem staging DACL = %q, want %q", got, want)
	}
}

func TestRetainedTempSecurityRejectsNullOrInsufficientDACL(t *testing.T) {
	if err := validateRetainedTempDACL(nil, localSystemSID); err == nil || !strings.Contains(err.Error(), "null DACL") {
		t.Fatalf("null staging DACL validation = %v", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FR;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRetainedTempDACL(dacl, localSystemSID); err == nil || !strings.Contains(err.Error(), "insufficient") {
		t.Fatalf("read-only staging DACL validation = %v", err)
	}
}
