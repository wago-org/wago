package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupProtectionDetectsReplacedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "protected")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	protection, err := captureCleanupProtection([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := protection.validate(); err == nil {
		t.Fatal("replaced protected directory accepted")
	}
}
