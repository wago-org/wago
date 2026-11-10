//go:build !wago_runtime

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainVersionDelegatesToManager(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	oldArgs, oldVersion, oldStdout := os.Args, version, os.Stdout
	t.Cleanup(func() {
		os.Args, version, os.Stdout = oldArgs, oldVersion, oldStdout
	})
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "--version"}
	version = "test-version"
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil || !strings.Contains(string(output), "test-version") {
		t.Fatalf("version output = %q, %v", output, err)
	}
}
