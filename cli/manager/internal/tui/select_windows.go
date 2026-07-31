//go:build windows

package tui

// runSelector's interactive raw-mode path is Unix-only (it toggles the terminal
// with stty). On Windows we report that no selection was submitted.
func Run(m selectorModel) (submitted, cancelled bool) {
	return false, false
}

func StdinIsTTY() bool { return false }
