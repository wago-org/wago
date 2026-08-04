package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAndDisplayPath(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "wago")
	cwd := filepath.Join(home, "code")

	if got, want := resolvePath("~"+string(os.PathSeparator)+"bin", cwd, home), filepath.Join(home, "bin"); got != want {
		t.Fatalf("resolve home path = %q, want %q", got, want)
	}
	if got, want := resolvePath("tools", cwd, home), filepath.Join(cwd, "tools"); got != want {
		t.Fatalf("resolve relative path = %q, want %q", got, want)
	}
	if got, want := displayPath(filepath.Join(home, "bin"), home), "~"+string(os.PathSeparator)+"bin"; got != want {
		t.Fatalf("display home path = %q, want %q", got, want)
	}
}

func TestPathSuggestionsOnlyReturnMatchingDirectories(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"tools", "tmp", "other"} {
		if err := os.Mkdir(filepath.Join(home, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "text.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := pathSuggestions("t", home, home)
	want := []string{
		"~" + string(os.PathSeparator) + "tmp" + string(os.PathSeparator),
		"~" + string(os.PathSeparator) + "tools" + string(os.PathSeparator),
	}
	if len(got) != len(want) {
		t.Fatalf("suggestions = %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("suggestions = %q, want %q", got, want)
		}
	}
}

func TestEnvironmentRadioUsesConfiguredDefaultWithoutConsole(t *testing.T) {
	t.Setenv("WAGO_UI_RADIO_TITLE", "Choose")
	t.Setenv("WAGO_UI_RADIO_ITEMS", "One|First|one|current\nTwo||two|")
	t.Setenv("WAGO_UI_RADIO_CURSOR", "1")

	value, ok := environmentRadio()
	if !ok || value != "two" {
		t.Fatalf("environment radio = %q, %v; want two, true", value, ok)
	}
}

func TestWriteSelectionUsesPortableNewline(t *testing.T) {
	output := filepath.Join(t.TempDir(), "selection")
	if err := writeSelection(output, "value"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "value\n"; got != want {
		t.Fatalf("selection bytes = %q, want %q", got, want)
	}
}

func TestColorsAreDisabledWithoutConsole(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if got := colorsFor(false); got != (style{}) {
		t.Fatalf("redirected output style = %#v, want no ANSI styling", got)
	}
}

func TestNoColorDisablesConsoleStyling(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := colorsFor(true); got != (style{}) {
		t.Fatalf("NO_COLOR style = %#v, want no ANSI styling", got)
	}
}

func TestProgressUsesAnimatedLoader(t *testing.T) {
	if len(spinnerFrames) < 2 {
		t.Fatalf("spinner frames = %q", spinnerFrames)
	}
	for _, frame := range spinnerFrames {
		if frame == "◇" {
			t.Fatal("progress still uses the static diamond")
		}
	}
}
