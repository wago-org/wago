package manager

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestManagerUsageShowsCommonFlagsOnly(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	managerUsage(writer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	want := `Wago is a wonderfully quick, compact, and extensible WebAssembly runtime for Go.

Usage: wago <command> [flags]

Commands:
  run       <file> [args...]       run a WebAssembly module (default)
  status                           show the runtime, project, plugins, and lockfile
  update                           update Wago, the runtime, and plugins
  init                             create a Wago project
  add       <module>[@version]...  add plugins and rebuild Wago
  rm        <name>                 remove a plugin
  plugin    <command>              manage and publish plugins
  auth      <command>              sign in to plugins.wago.sh
  module    <command>              inspect module imports and capabilities
  self      <command>              update or uninstall Wago
  compile   <file>                 build a standalone executable
  build     <file>                 precompile a module to .wago
  validate  <file>                 validate a WebAssembly module
  version   <command>              manage Wago runtimes
  cache     <command>              inspect and clean cached data
  config    <command>              configure Wago
  commands                         describe commands as JSON

Flags:
  --version, -v               show version information
  --help, -h                  show this help
  --json, -j                  emit JSON when supported

Repository:  https://github.com/wago-org/wago
Plugins:     https://plugins.wago.sh
`
	if got := string(output); got != want {
		t.Fatalf("manager usage:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestDisplayPathUsesHomeShorthand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}

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
