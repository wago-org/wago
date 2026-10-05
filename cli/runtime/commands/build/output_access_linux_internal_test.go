//go:build linux

package build

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidateLinuxBuildOutputAttributeValues(t *testing.T) {
	attributes := []uint64{
		unix.STATX_ATTR_VERITY,
		unix.STATX_ATTR_IMMUTABLE,
		unix.STATX_ATTR_APPEND,
		unix.STATX_ATTR_COMPRESSED,
		unix.STATX_ATTR_ENCRYPTED,
		unix.STATX_ATTR_NODUMP,
		unix.STATX_ATTR_DAX,
	}
	for _, attribute := range attributes {
		if err := validateLinuxBuildOutputAttributeValues(attribute, attribute); err == nil {
			t.Fatalf("reported persistent attribute %#x accepted", attribute)
		}
		if err := validateLinuxBuildOutputAttributeValues(attribute, 0); err != nil {
			t.Fatalf("filesystem-unsupported attribute %#x rejected: %v", attribute, err)
		}
	}
	if err := validateLinuxBuildOutputAttributeValues(0, linuxBuildOutputPersistentAttributes); err != nil {
		t.Fatalf("clear persistent attributes rejected: %v", err)
	}
}

func TestLinuxXattrNeedsUpdateSkipsIdenticalValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "staged.wago")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	const name = "user.wago-identical-label"
	want := []byte("same-label")
	if err := unix.Fsetxattr(int(file.Fd()), name, want, 0); err != nil {
		t.Skipf("extended attributes unavailable: %v", err)
	}
	update, err := linuxXattrNeedsUpdate(int(file.Fd()), name, want, 4<<10)
	if err != nil {
		t.Fatal(err)
	}
	if update {
		t.Fatal("identical access label unnecessarily requires a metadata write")
	}
	update, err = linuxXattrNeedsUpdate(int(file.Fd()), name, []byte("different-label"), 4<<10)
	if err != nil {
		t.Fatal(err)
	}
	if !update {
		t.Fatal("different access label did not request a metadata write")
	}
}
