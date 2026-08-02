//go:build windows

package tui

import "github.com/wago-org/wago/cli/internal/automation"

// runSelector's interactive raw-mode path is Unix-only (it toggles the terminal
// with stty). On Windows we report that no selection was submitted.
func Run(m selectorModel) (submitted, cancelled bool) {
	return false, false
}

func StdinIsTTY() bool {
	if automation.NoInput() {
		return false
	}
	return false
}
