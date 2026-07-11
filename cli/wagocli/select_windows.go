//go:build windows

package wagocli

// runSelector's interactive raw-mode path is Unix-only (it toggles the terminal
// with stty). On Windows we skip the interactive picker and keep the caller's
// pre-seeded selection, returning not-cancelled.
func runSelector(m *multiSelect) (cancelled bool) {
	return false
}
