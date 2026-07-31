package registry

import "github.com/wago-org/wago/cli/internal/ui"

func fatal(format string, args ...any) { ui.Fatal(format, args...) }
func bold(value string) string         { return ui.Bold(value) }
func dim(value string) string          { return ui.Dim(value) }
func cyan(value string) string         { return ui.Cyan(value) }
