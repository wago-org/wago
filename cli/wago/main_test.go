package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainVersionDelegatesToCLI(t *testing.T) {
	oldArgs, oldVersion, oldStdout := os.Args, version, os.Stdout
	t.Cleanup(func() {
		os.Args, version, os.Stdout = oldArgs, oldVersion, oldStdout
	})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	os.Args = []string{"wago", "--version"}
	version = "test-version"
	main()
	_ = w.Close()
	b, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !strings.Contains(string(b), "test-version") {
		t.Fatalf("version output = %q, %v", b, err)
	}
}
