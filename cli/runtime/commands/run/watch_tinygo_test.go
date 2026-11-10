//go:build linux && tinygo && !wago_lean

package run

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWithoutWatchFlagsPreservesGuestArguments(t *testing.T) {
	input := []string{"--invoke", "helper.wasm", "--watch", "--watch-interval", "1s", "module.wasm", "--watch", "guest"}
	want := []string{"run", "--invoke", "helper.wasm", "module.wasm", "--watch", "guest"}
	if got := watchedChildArguments(input, Command(testEnvironment{}).AllFlags()); !reflect.DeepEqual(got, want) {
		t.Fatalf("watchedChildArguments = %#v, want %#v", got, want)
	}
}

func TestFileStampDetectsContentRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module.wasm")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := fileStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("final"), 0o600); err != nil {
		t.Fatal(err)
	}
	final, err := fileStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == final {
		t.Fatal("content rewrite was not detected")
	}
}
