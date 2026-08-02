//go:build !wago_runtime && !windows

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

func TestManagerForwardsPluginCommandsToSelectedRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	config := filepath.Join(root, "config")
	runner := filepath.Join(root, "data", "versions", "canary", "standard", "normal", "wago-runtime")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"active-version": "canary\n",
		"active-profile": "standard\n",
		"active-build":   "normal\n",
	} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nprintf 'runtime:%s\\n' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "plugins", "list", "--global"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != "runtime:plugins list --global" {
		t.Fatalf("manager plugin forwarding = %q", got)
	}
}

func TestManagerOwnsPluginLifecycleWithoutSelectedRuntime(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "add", "--help"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "Usage: wago add <module>[@version]...") {
		t.Fatalf("manager add help = %q", text)
	}
}

func TestManagerOwnsPluginIntrospectionHelpWithoutSelectedRuntime(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "plugin", "list", "--help"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "Usage: wago plugin list") {
		t.Fatalf("manager plugin list help = %q", text)
	}
}

func TestManagerRunRedirectsToVersionInstallWithoutSelectedRunner(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"wago", "run", "module.wasm"}
	main()

	joined := strings.Join(requests, "\n")
	for _, want := range []string{"/repos/wago-org/wago/releases", "/repos/wago-org/wago/commits"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("first run did not enter version install; missing request %q in:\n%s", want, joined)
		}
	}
}

func TestManagerOwnsTopLevelHelp(t *testing.T) {
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
	script := "#!/bin/sh\nprintf 'stale runtime help\\n'\n"
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
	for _, want := range []string{
		"Usage: wago <command> [flags]",
		"update or uninstall Wago",
		"View the registry:           https://plugins.wago.sh",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manager help missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "stale runtime help") {
		t.Fatalf("top-level help delegated to the selected runtime:\n%s", text)
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

func TestManagerOwnsInitWithoutSelectedRunner(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	os.Args = []string{"wago", "init", "--run"}
	main()
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "Initialized wago.json") {
		t.Fatalf("manager init output = %q", output)
	}
	manifest, err := os.ReadFile(filepath.Join(project, "wago.json"))
	if err != nil ||
		!strings.Contains(string(manifest), `"$schema": "https://wago.sh/v0/schema.json"`) ||
		strings.Contains(string(manifest), `"schema":`) {
		t.Fatalf("manager init manifest = %q, %v", manifest, err)
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
