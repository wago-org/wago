package plugin

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestLegacyGlobalPluginsMigrateToSharedIntent(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	sourceDir := filepath.Join(dirs.Versions, "canary-source123", "plugins")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"$schema":"https://wago.sh/v0/schema.json","plugins":{"wago-org/wasi":"^0.0.0"}}`)
	lock := []byte(`{"plugins":{"wago-org/wasi":{"version":"v0.0.0","requiredCapabilities":["host.environment"],"capabilities":["host.environment"]}}}`)
	if err := os.WriteFile(filepath.Join(sourceDir, project.File), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "wago-lock.json"), lock, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyGlobalPlugins(dirs, "canary-source123"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{project.File: manifest, "wago-lock.json": lock} {
		got, err := os.ReadFile(filepath.Join(dirs.Data, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("migrated %s = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(sourceDir, project.File)); err != nil {
		t.Fatalf("legacy manifest should remain recoverable: %v", err)
	}
}
