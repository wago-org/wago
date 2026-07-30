//go:build windows

package wagocli

// runSelector's interactive raw-mode path is Unix-only (it toggles the terminal
// with stty). On Windows we report that no selection was submitted.
func runSelector(m selectorModel) (submitted, cancelled bool) {
	return false, false
}

func stdinIsTTY() bool { return false }
