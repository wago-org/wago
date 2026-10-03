package pluginmenu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/tui"
)

func TestPickerDrillsIntoSubpackagesAndReturns(t *testing.T) {
	picker := Picker("Select installed plugin", []Package{
		{Name: "wago-org/wasi", Version: "0.0.0"},
		{Name: "wago-org/wasi/p1", Version: "0.0.0"},
		{Name: "wago-org/wasi/unstable", Version: "0.0.0"},
	})
	frame := picker.Frame()
	if !strings.Contains(frame, "wago-org/wasi") || !strings.Contains(frame, "→") {
		t.Fatalf("root frame:\n%s", frame)
	}
	if done, cancelled := picker.Apply(tui.KeyRight); done || cancelled {
		t.Fatalf("right = done %v, cancelled %v", done, cancelled)
	}
	frame = picker.Frame()
	for _, want := range []string{"wasi/p1", "wasi/unstable", "←/esc back"} {
		if !strings.Contains(frame, want) {
			t.Errorf("child frame does not contain %q:\n%s", want, frame)
		}
	}
	if done, cancelled := picker.Apply(tui.KeyLeft); done || cancelled || picker.Depth() != 1 {
		t.Fatalf("left = done %v, cancelled %v, depth %d", done, cancelled, picker.Depth())
	}
}

func TestPickerAcceptsCanonicalGitHubPackage(t *testing.T) {
	const name = "github.com/wago-org/wasi"
	picker := Picker("Select installed plugin", []Package{{Name: name, Version: "0.0.0"}})
	if done, cancelled := picker.Apply(tui.KeyAccept); !done || cancelled {
		t.Fatalf("enter = done %v, cancelled %v", done, cancelled)
	}
	if got := picker.Selected(); got != name {
		t.Fatalf("selected = %q, want %q", got, name)
	}
}

func TestPickerGroupsCanonicalSubpackages(t *testing.T) {
	for _, test := range []struct {
		root, child, label string
	}{
		{"github.com/wago-org/wasi", "github.com/wago-org/wasi/p1", "wasi/p1"},
		{"gopkg.in/yaml.v3", "gopkg.in/yaml.v3/parser", "yaml.v3/parser"},
	} {
		t.Run(test.root, func(t *testing.T) {
			picker := Picker("Select installed plugin", []Package{
				{Name: test.root, Version: "1.0.0"},
				{Name: test.child, Version: "1.0.0"},
			})
			if frame := picker.Frame(); !strings.Contains(frame, test.root) || !strings.Contains(frame, "→") {
				t.Fatalf("root frame:\n%s", frame)
			}
			if done, cancelled := picker.Apply(tui.KeyRight); done || cancelled {
				t.Fatalf("right = done %v, cancelled %v", done, cancelled)
			}
			if frame := picker.Frame(); !strings.Contains(frame, test.label) {
				t.Fatalf("child frame:\n%s", frame)
			}
		})
	}
}

func TestPickerPaginatesAtFifteenRoots(t *testing.T) {
	packages := make([]Package, 17)
	for index := range packages {
		packages[index].Name = fmt.Sprintf("org/plugin-%02d", index)
	}
	picker := Picker("Plugins", packages)
	for range 14 {
		picker.MoveDown()
	}
	if frame := picker.Frame(); !strings.Contains(frame, "plugin-00") || strings.Contains(frame, "plugin-15") {
		t.Fatalf("picker changed page before row 15:\n%s", frame)
	}
	picker.MoveDown()
	if frame := picker.Frame(); strings.Contains(frame, "plugin-14") || !strings.Contains(frame, "plugin-15") {
		t.Fatalf("picker did not change page at row 16:\n%s", frame)
	}
}
