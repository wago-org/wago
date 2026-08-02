package manager

import (
	"path/filepath"
	"testing"
)

func TestDisplayPathUsesHomeShorthand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)

	if got, want := displayPath(filepath.Join(home, ".local", "share", "wago")), filepath.Join("~", ".local", "share", "wago"); got != want {
		t.Fatalf("displayPath() = %q, want %q", got, want)
	}
	if got := displayPath(home); got != "~" {
		t.Fatalf("displayPath(home) = %q, want ~", got)
	}
	outside := filepath.Join(t.TempDir(), "wago")
	if got := displayPath(outside); got != outside {
		t.Fatalf("displayPath(outside) = %q, want unchanged", got)
	}
}
