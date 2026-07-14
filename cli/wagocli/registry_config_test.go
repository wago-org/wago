package wagocli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryConfigAndCredentials(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("HOME", filepath.Join(config, "home"))
	t.Setenv("WAGO_REGISTRY", " https://api.example.test/ ")
	t.Setenv("WAGO_TOKEN", "")

	if got := registryBase(); got != "https://api.example.test" {
		t.Fatalf("registryBase = %q", got)
	}
	if got := frontendBase(); got != "https://example.test" {
		t.Fatalf("frontendBase = %q", got)
	}
	if got := shortFromModule("github.com/acme/wago_test"); got != "test" {
		t.Fatalf("shortFromModule = %q", got)
	}
	if got := ownerFromModule("github.com/acme/plugin"); got != "acme" {
		t.Fatalf("ownerFromModule = %q", got)
	}
	if got := ownerFromModule("example.test/acme/plugin"); got != "" {
		t.Fatalf("non-GitHub owner = %q", got)
	}
	if got := packageURL("github.com/acme/wago_test"); got != "https://example.test/acme/test" {
		t.Fatalf("packageURL = %q", got)
	}
	if got := credentialsPath(); got != filepath.Join(config, "wago", "credentials.json") {
		t.Fatalf("credentialsPath = %q", got)
	}
	if creds, err := loadCredentials(); err != nil || len(creds) != 0 {
		t.Fatalf("load missing = %#v, %v", creds, err)
	}
	if err := saveCredentials(registryBase(), "stored", "alice"); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}
	if got := resolveToken(); got != "stored" {
		t.Fatalf("stored resolveToken = %q", got)
	}
	if info, err := os.Stat(credentialsPath()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %v, %v", info, err)
	}
	t.Setenv("WAGO_TOKEN", " environment ")
	if got := resolveToken(); got != "environment" {
		t.Fatalf("environment resolveToken = %q", got)
	}
	t.Setenv("WAGO_TOKEN", "")
	if err := deleteCredentials(registryBase()); err != nil {
		t.Fatalf("deleteCredentials: %v", err)
	}
	if got := resolveToken(); got != "" {
		t.Fatalf("deleted resolveToken = %q", got)
	}
	if err := deleteCredentials(registryBase()); err != nil {
		t.Fatalf("repeat deleteCredentials: %v", err)
	}
}

func TestRegistryConfigDefaultsAndMalformedCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WAGO_REGISTRY", "")
	t.Setenv("WAGO_TOKEN", "")
	if got := registryBase(); got != defaultRegistry {

		t.Fatalf("default registry = %q", got)
	}
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath(), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCredentials(); err == nil {
		t.Fatal("malformed credentials accepted")
	}
	if got := resolveToken(); got != "" {
		t.Fatalf("malformed resolveToken = %q", got)
	}
	if err := saveCredentials(defaultRegistry, "x", "x"); err == nil {
		t.Fatal("save accepted malformed existing credentials")
	}
	if err := deleteCredentials(defaultRegistry); err == nil {
		t.Fatal("delete accepted malformed existing credentials")
	}
}

func TestRegistryConfigRemainingBranches(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", "")
	t.Setenv("WAGO_REGISTRY", "localhost:8080")
	if got := frontendBase(); got != "localhost:8080" {
		t.Fatalf("non-HTTP frontendBase = %q", got)
	}
	t.Setenv("WAGO_REGISTRY", "http://api.localhost:8080")
	if got := frontendBase(); got != "http://localhost:8080" {
		t.Fatalf("HTTP frontendBase = %q", got)
	}
	if got := ownerFromModule("github.com/acme"); got != "" {
		t.Fatalf("owner without repository = %q", got)
	}
	if got := packageURL("not-a-module"); got != "http://localhost:8080/packages/not-a-module" {
		t.Fatalf("fallback packageURL = %q", got)
	}
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath(), []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if creds, err := loadCredentials(); err != nil || len(creds) != 0 {
		t.Fatalf("load blank = %#v, %v", creds, err)
	}
	if err := writeCredentials(map[string]credential{"b": {Token: "b"}, "a": {Login: "a"}}); err != nil {
		t.Fatalf("writeCredentials: %v", err)
	}
	creds, err := loadCredentials()
	if err != nil || creds["b"].Token != "b" || creds["a"].Login != "a" {
		t.Fatalf("round-trip credentials = %#v, %v", creds, err)
	}

	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	if err := writeCredentials(map[string]credential{}); err == nil {
		t.Fatal("writeCredentials accepted a file as config directory")
	}
}
