//go:build wago_manager && !windows

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerLaunchesSelectedRunner(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	config := filepath.Join(root, "config")
	runner := filepath.Join(root, "data", "versions", "canary", "minimal", "wago-runner")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "active-version"), []byte("canary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "active-profile"), []byte("minimal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nprintf 'runner:%s\\n' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "run", "--invoke", "fib", "module.wasm", "20"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != "runner:run --invoke fib module.wasm 20" {
		t.Fatalf("manager output = %q", got)
	}
}

func TestManagerVersionUpgradesLegacyRunnerOutput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	config := filepath.Join(root, "config")
	runner := filepath.Join(root, "data", "versions", "canary", "standard", "wago-runner")
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"active-version": "canary\n",
		"active-profile": "standard\n",
	} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nprintf 'wago 96042ee (linux/amd64)\\nfeatures: simd|multi-value\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldArgs, oldVersion, oldStdout := os.Args, version, os.Stdout
	t.Cleanup(func() {
		os.Args, version, os.Stdout = oldArgs, oldVersion, oldStdout
	})
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "-v"}
	version = "manager-test"
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		"channel      canary",
		"release      96042ee",
		"profile      standard",
		"platform     linux/amd64",
		"manager      manager-test",
		"plugins      unavailable",
		"features     simd|multi-value",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manager report missing %q:\n%s", want, text)
		}
	}
}
