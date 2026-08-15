package plugin

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
)

func TestEmptyMaintenanceReportsSayNoPluginsEnabled(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	t.Setenv("WAGO_HOME", t.TempDir())
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.WriteFile("wago.json", []byte("{\n  \"$schema\": \"https://wago.sh/v1/schema.json\",\n  \"plugins\": {}\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func(){
		"outdated": func() { Outdated(MaintenanceRequest{}) },
		"tree":     func() { Tree(MaintenanceRequest{}) },
	} {
		t.Run(name, func(t *testing.T) {
			old := os.Stdout
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = writer
			run()
			_ = writer.Close()
			os.Stdout = old
			output, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "no plugins enabled") {
				t.Fatalf("empty %s output = %q", name, output)
			}
		})
	}
}
