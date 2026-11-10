//go:build !linux && !darwin

package main

// Directory fsync is not portable to this host. File contents and atomic
// renames are still used; supported Unix maintenance hosts additionally sync
// parent directories.
func syncDir(string) error { return nil }
