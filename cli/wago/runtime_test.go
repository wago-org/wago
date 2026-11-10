//go:build wago_runtime

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRuntimeEntrypointDelegatesToRuntimeCLI(t *testing.T) {
	oldArgs, oldVersion, oldStdout := os.Args, version, os.Stdout
	t.Cleanup(func() {
		os.Args, version, os.Stdout = oldArgs, oldVersion, oldStdout
	})
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago-runtime", "--version"}
	version = "runtime-test"
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "runtime-test") {
		t.Fatalf("runtime version output = %q", output)
	}
}
