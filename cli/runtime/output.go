package runtime

import "github.com/wago-org/wago/cli/internal/ui"

func bold(value string) string { return ui.Bold(value) }
func dim(value string) string  { return ui.Dim(value) }
func red(value string) string  { return ui.Red(value) }
