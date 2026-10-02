//go:build windows

package windowsfilepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAddExtendedPrefix(t *testing.T) {
	tests := []struct {
		name string
		path string
		want func(string) string
	}{
		{name: "empty", path: "", want: func(path string) string { return path }},
		{name: "short", path: `C:\short\artifact.wago`, want: func(path string) string { return path }},
		{name: "extended", path: `\\?\C:\` + strings.Repeat("a", 260), want: func(path string) string { return path }},
		{name: "native-extended", path: `\??\C:\` + strings.Repeat("a", 260), want: func(path string) string { return path }},
		{name: "device", path: `\\.\C:\` + strings.Repeat("a", 260), want: func(path string) string { return path }},
		{name: "drive", path: `C:\` + strings.Repeat("a", 260), want: func(path string) string {
			full, err := windows.FullPath(path)
			if err != nil {
				t.Fatal(err)
			}
			return `\\?\` + full
		}},
		{name: "unc", path: `\\server\share\` + strings.Repeat("a", 260), want: func(path string) string {
			full, err := windows.FullPath(path)
			if err != nil {
				t.Fatal(err)
			}
			return `\\?\UNC\` + full[2:]
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := addExtendedPrefix(test.path), test.want(test.path); got != want {
				t.Fatalf("addExtendedPrefix(%q) = %q, want %q", test.path, got, want)
			}
		})
	}
}

func TestAddExtendedPrefixUsesAbsolutePathWhenWorkingDirectoryMakesItLong(t *testing.T) {
	dir := t.TempDir()
	for len(dir) < extendedPathThreshold {
		dir = filepath.Join(dir, strings.Repeat("working-directory-", 4))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	got := addExtendedPrefix(`artifact.wago`)
	if !strings.HasPrefix(got, `\\?\`) || !strings.HasSuffix(got, `\artifact.wago`) {
		t.Fatalf("extended relative path = %q", got)
	}
}

func TestUTF16PtrFromStringRejectsNUL(t *testing.T) {
	if _, err := UTF16PtrFromString("artifact\x00.wago"); err == nil {
		t.Fatal("UTF16PtrFromString accepted a NUL-containing path")
	}
}
