package wagocli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s "+format+"\n", append([]any{red("wago:")}, args...)...)
	os.Exit(1)
}

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

func bold(s string) string { return paint("1", s) }
func dim(s string) string  { return paint("2", s) }
func red(s string) string  { return paint("31", s) }
func cyan(s string) string { return paint("36", s) }

func printVersionDetail(label, value string) {
	printDetail(os.Stdout, label, value)
}

func printDetail(out io.Writer, label, value string) {
	fmt.Fprintf(out, "  %s %s\n", dim(fmt.Sprintf("%-12s", label)), value)
}

func displayPath(path string) string {
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
