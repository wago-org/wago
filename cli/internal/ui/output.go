// Package ui provides the shared terminal presentation used by both Wago CLIs.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
)

var useColor = colorEnabled()

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Styling is CLI presentation work. Share its plumbing across command callers
// rather than expanding it into each command body.
//
//go:noinline
func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

//go:noinline
func Bold(s string) string { return paint("1", s) }

//go:noinline
func Dim(s string) string { return paint("2", s) }

//go:noinline
func Red(s string) string { return paint("31", s) }

//go:noinline
func Cyan(s string) string { return paint("36", s) }

// Failure forwarding is a cold, terminating path. Keep formatting and exit
// behavior shared across the CLI's many error sites.
//
//go:noinline
func Fatal(format string, args ...any) {
	Fail(1, "operational_error", format, args...)
}

//go:noinline
func Usage(format string, args ...any) {
	Fail(2, "usage_error", format, args...)
}

//go:noinline
func FatalHint(hint, format string, args ...any) {
	FailHint(1, "operational_error", hint, format, args...)
}

//go:noinline
func UsageHint(hint, format string, args ...any) {
	FailHint(2, "usage_error", hint, format, args...)
}

//go:noinline
func Fail(exitCode int, code, format string, args ...any) {
	FailHint(exitCode, code, "", format, args...)
}

func FailHint(exitCode int, code, hint, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if automation.JSON() {
		details := map[string]any{"code": code, "message": message}
		if hint != "" {
			details["hint"] = hint
		}
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{
			"error": details,
		})
	} else {
		fmt.Fprintf(os.Stderr, "%s %s\n", Red("wago:"), message)
		if hint != "" {
			fmt.Fprintf(os.Stderr, "%s %s\n", Dim("hint:"), hint)
		}
	}
	os.Exit(exitCode)
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
