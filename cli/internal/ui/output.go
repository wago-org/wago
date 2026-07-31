// Package ui provides the shared terminal presentation used by both Wago CLIs.
package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var useColor = colorEnabled()

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Bold(s string) string { return paint("1", s) }
func Dim(s string) string  { return paint("2", s) }
func Red(s string) string  { return paint("31", s) }
func Cyan(s string) string { return paint("36", s) }

func Fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s "+format+"\n", append([]any{Red("wago:")}, args...)...)
	os.Exit(1)
}

func Detail(out io.Writer, label, value string) {
	fmt.Fprintf(out, "  %s %s\n", Dim(fmt.Sprintf("%-12s", label)), value)
}

func DisplayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	path, home = filepath.Clean(path), filepath.Clean(home)
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
