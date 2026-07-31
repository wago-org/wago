package manager

import (
	"io"
	"os"

	"github.com/wago-org/wago/cli/internal/ui"
)

func fatal(format string, args ...any) { ui.Fatal(format, args...) }
func bold(value string) string         { return ui.Bold(value) }
func dim(value string) string          { return ui.Dim(value) }

func printVersionDetail(label, value string) {
	printDetail(os.Stdout, label, value)
}

func printDetail(out io.Writer, label, value string) {
	ui.Detail(out, label, value)
}

func displayPath(path string) string {
	return ui.DisplayPath(path)
}
