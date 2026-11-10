//go:build windows

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCountedWindowsCacheFileSkipsDirectoriesAndReparsePoints(t *testing.T) {
	tests := []struct {
		name       string
		attributes uint32
		want       bool
	}{
		{name: "regular file", want: true},
		{name: "directory", attributes: windows.FILE_ATTRIBUTE_DIRECTORY},
		{name: "file symlink", attributes: windows.FILE_ATTRIBUTE_REPARSE_POINT},
		{name: "directory junction", attributes: windows.FILE_ATTRIBUTE_DIRECTORY | windows.FILE_ATTRIBUTE_REPARSE_POINT},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := windows.ByHandleFileInformation{FileAttributes: test.attributes}
			if got := countedWindowsCacheFile(info); got != test.want {
				t.Fatalf("countedWindowsCacheFile(attributes %#x) = %v, want %v", test.attributes, got, test.want)
			}
		})
	}
}

func TestCanonicalCachePathContainmentUsesNormalizedHandleSpelling(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "MixedCase", "Versions")
	childPath := filepath.Join(rootPath, "v1")
	if err := os.MkdirAll(childPath, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := openCacheObject(strings.ToUpper(rootPath), false)
	if err != nil {
		t.Skipf("temporary volume is case-sensitive: %v", err)
	}
	defer root.Close()
	rootName, err := finalCachePath(root)
	if err != nil {
		t.Fatal(err)
	}
	child, err := openCacheObject(childPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	contained, err := cacheHandleContained(child, rootName)
	if err != nil {
		t.Fatal(err)
	}
	if !contained {
		t.Fatalf("normalized child handle escaped normalized root %q", rootName)
	}
}

func TestCleanupHandlePreventsPostValidationMove(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "plugins")
	if err := os.WriteFile(leaf, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "moved")
	file, err := openCacheObject(leaf, true)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// A handle-bound delete is only contained if the opened object cannot be
	// moved outside the checked cache tree between validation and deletion.
	if err := os.Rename(leaf, outside); err == nil {
		t.Fatal("an opened cleanup target was moved outside the cache tree")
	}
}

func TestCleanupDirectoryHandlePreventsPostValidationMove(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "version")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "moved")
	file, err := openCacheObject(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// Each held ancestor must remain in the checked tree until its children
	// have been deleted through their own validated handles.
	if err := os.Rename(dir, outside); err == nil {
		t.Fatal("an opened cleanup ancestor was moved outside the cache tree")
	}
}

func TestCleanupRejectsReplacedRootParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "cache")
	versions := filepath.Join(parent, "versions")
	original := filepath.Join(versions, "v1", "standard", "normal", "plugins")
	if err := os.MkdirAll(original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "cache.wago"), []byte("original cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalParent := filepath.Join(base, "external")
	external := filepath.Join(externalParent, "versions", "v1", "standard", "normal", "plugins")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(external, "cache.wago")
	if err := os.WriteFile(marker, []byte("external data"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Allow this test to perform the adversarial parent swap even on native
	// Windows; production cleanup deliberately denies delete sharing.
	root, err := openCacheObjectWithSharing(versions, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rootName, err := finalCachePath(root)
	if err != nil {
		t.Fatal(err)
	}
	// Move the opened tree away through its parent, then put a pre-existing
	// external tree at the same pathname with matching child names.
	if err := os.Rename(parent, filepath.Join(base, "moved-cache")); err != nil {
		// Native NTFS refuses this particular parent move even with delete
		// sharing, while Wine allows it. Test the substitution only where the
		// filesystem permits the adversarial setup; production still opens
		// every child relative to the held parent handle on either platform.
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Skipf("filesystem prevents parent swap while versions is open: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.Rename(externalParent, parent); err != nil {
		t.Fatal(err)
	}
	_, _ = visitWindowsBuildLevel(root, versions, rootName, 0, true)
	if _, err := os.Stat(filepath.Join(parent, "versions", "v1", "standard", "normal", "plugins", "cache.wago")); err != nil {
		t.Fatalf("cleanup deleted a substituted external tree: %v", err)
	}
}

func TestRelativeCacheOpenRejectsInvalidEntryNames(t *testing.T) {
	parent, err := openCacheObject(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	for _, name := range []string{"", ".", "..", `sub\child`, "sub/child", "file:stream"} {
		if child, err := openCacheChild(parent, name, name, false, true); err == nil {
			_ = child.Close()
			t.Fatalf("opened invalid child component %q", name)
		}
	}
	if _, err := openCacheChild(parent, "missing", "missing", false, true); !os.IsNotExist(err) {
		t.Fatalf("missing child error = %v, want os.IsNotExist", err)
	}
}

func TestReadonlyPluginLeafIsDeletedOrFailClosed(t *testing.T) {
	versions := filepath.Join(t.TempDir(), "versions")
	leaf := filepath.Join(versions, "v1", "standard", "normal", "plugins")
	if err := os.MkdirAll(filepath.Dir(leaf), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(leaf)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(name, windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatal(err)
	}
	result, err := cleanPluginBuilds(versions)
	if err != nil {
		// On filesystems without disposition flags that ignore READONLY, a
		// failed delete must leave the leaf and not count its bytes.
		if result.Bytes != 0 || result.Removed != 0 {
			t.Fatalf("failed readonly deletion was counted: %+v (%v)", result, err)
		}
		if _, statErr := os.Stat(leaf); statErr != nil {
			t.Fatalf("failed deletion lost readonly leaf: %v", statErr)
		}
		return
	}
	if result.Bytes != 5 || result.Removed != 1 {
		t.Fatalf("readonly deletion result = %+v, want 5 bytes and 1 leaf", result)
	}
	if _, err := os.Stat(leaf); !os.IsNotExist(err) {
		t.Fatalf("readonly leaf remains after reported deletion: %v", err)
	}
}
