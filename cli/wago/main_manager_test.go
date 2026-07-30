//go:build wago_manager && !windows

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func TestManagerDelegatesTopLevelHelpWithManagerContext(t *testing.T) {
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
	for name, value := range map[string]string{
		"active-version": "canary\n",
		"active-profile": "minimal\n",
	} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nprintf 'args:%s\\nmanager:%s\\nexecutable:%s\\n' \"$*\" \"$WAGO_MANAGER_VERSION\" \"$WAGO_MANAGER_EXECUTABLE\"\n"
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
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
	os.Args = []string{"wago", "--help"}
	version = "manager-test"
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{"args:--help", "manager:manager-test", "executable:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("delegated help missing %q:\n%s", want, text)
		}
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

func TestManagerOwnsAuthWithoutSelectedRunner(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "auth", "whoami"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != "not logged in (run: wago auth login)" {
		t.Fatalf("manager auth output = %q", got)
	}
}

func TestManagerAuthLoginStoresCredentialWithoutSelectedRunner(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" || r.Header.Get("Authorization") != "Bearer manager-token" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"login":"alice"}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "auth", "login", "--token", "manager-token"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "Logged in as alice") {
		t.Fatalf("manager auth output = %q", output)
	}
	credentials, err := os.ReadFile(filepath.Join(root, "config", "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{server.URL, "manager-token", "alice"} {
		if !strings.Contains(string(credentials), want) {
			t.Fatalf("credentials missing %q: %s", want, credentials)
		}
	}
}
