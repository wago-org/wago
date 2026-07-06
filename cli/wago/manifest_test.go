package main

import (
	"path/filepath"
	"testing"
)

func TestManifestAddRemoveSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wago-plugins.json")

	m, err := loadManifest(path)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(m.Plugins) != 0 || m.Version != manifestVersion {
		t.Fatalf("empty manifest = %+v", m)
	}

	if newly := m.add(PluginEntry{Name: "redis", Module: "github.com/acme/wago-redis", Version: "v0.3.1", Enabled: true}); !newly {
		t.Fatal("add should report newly-added")
	}
	m.add(PluginEntry{Name: "timer", Module: "builtin", Enabled: true})
	// Re-adding updates in place (not newly).
	if newly := m.add(PluginEntry{Name: "redis", Module: "github.com/acme/wago-redis", Version: "v0.4.0", Enabled: true}); newly {
		t.Fatal("re-add should update, not add")
	}
	if err := m.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload and verify contents + stable (name-sorted) order.
	m2, err := loadManifest(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(m2.Plugins) != 2 {
		t.Fatalf("reloaded %d plugins, want 2", len(m2.Plugins))
	}
	if m2.Plugins[0].Name != "redis" || m2.Plugins[1].Name != "timer" {
		t.Fatalf("order = %s,%s want redis,timer", m2.Plugins[0].Name, m2.Plugins[1].Name)
	}
	if m2.Plugins[0].Version != "v0.4.0" {
		t.Fatalf("redis version = %q, want v0.4.0 (updated)", m2.Plugins[0].Version)
	}
	if !m2.Plugins[1].Builtin() {
		t.Fatal("timer entry should be builtin")
	}

	if !m2.remove("redis") {
		t.Fatal("remove existing should return true")
	}
	if m2.remove("nope") {
		t.Fatal("remove missing should return false")
	}
	if len(m2.Plugins) != 1 || m2.Plugins[0].Name != "timer" {
		t.Fatalf("after remove = %+v", m2.Plugins)
	}
}

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"github.com/acme/wago-redis": "redis",
		"github.com/acme/wago_kv":    "kv",
		"example.com/foo/bar":        "bar",
		"wago-auth":                  "auth",
	}
	for module, want := range cases {
		if got := deriveName(module); got != want {
			t.Fatalf("deriveName(%q) = %q, want %q", module, got, want)
		}
	}
}
