package wagocli

import (
	"fmt"
	"strings"
)

// select.go is a tiny, dependency-free multi-select: a cursor over a list of
// toggleable rows. The model here is pure (no terminal I/O) so it's fully
// unit-testable; the raw-mode driver that feeds it keypresses and paints frames
// lives in select_unix.go / select_windows.go.

// selItem is one toggleable row.
type selItem struct {
	label string // machine value, e.g. a capability id "wasi:stdio"
	desc  string // one-line human description (may be empty)
	on    bool   // currently selected
}

// selectKey is a normalized keypress the model understands.
type selectKey int

const (
	keyNoop selectKey = iota // unrecognized / intentionally inert (e.g. ← →)
	keyUp
	keyDown
	keyToggle // space
	keyAll    // a
	keyClear  // n
	keyAccept // enter / return
	keyCancel // esc / q / ctrl-c
)

// multiSelect is the pure picker state: a list plus a cursor. When reject is
// set, a trailing "Reject All" row sits after the items; submitting on it sets
// rejected. prompt overrides the default footer hint.
type multiSelect struct {
	title    string
	prompt   string
	items    []selItem
	cursor   int
	reject   bool // show a trailing "Reject All" row
	rejected bool // submitted while on the Reject All row
}

// lastRow is the index of the final navigable row (the Reject All row when
// present, otherwise the last item).
func (m *multiSelect) lastRow() int {
	if m.reject {
		return len(m.items)
	}
	return len(m.items) - 1
}

// apply advances the model by one key. It reports whether the interaction is
// finished, and if so whether it was cancelled (esc) rather than submitted
// (enter). Movement clamps at the ends; ← and → are intentionally inert.
func (m *multiSelect) apply(k selectKey) (done, cancelled bool) {
	switch k {
	case keyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case keyDown:
		if m.cursor < m.lastRow() {
			m.cursor++
		}
	case keyToggle:
		if m.cursor < len(m.items) { // no-op on the Reject All row
			m.items[m.cursor].on = !m.items[m.cursor].on
		}
	case keyAll:
		for i := range m.items {
			m.items[i].on = true
		}
	case keyClear:
		for i := range m.items {
			m.items[i].on = false
		}
	case keyAccept:
		if m.reject && m.cursor == len(m.items) {
			m.rejected = true // submitted on Reject All
		}
		return true, false
	case keyCancel:
		return true, true
	}
	return false, false
}

// chosen returns the labels of the selected rows, in list order.
func (m *multiSelect) chosen() []string {
	var out []string
	for _, it := range m.items {
		if it.on {
			out = append(out, it.label)
		}
	}
	return out
}

// decodeKey maps a raw input chunk (one keypress, possibly a multi-byte escape
// sequence for arrows) to a selectKey. Kept pure and separate so key handling is
// testable without a terminal.
func decodeKey(b []byte) selectKey {
	switch {
	case len(b) == 0:
		return keyNoop
	case len(b) == 1:
		switch b[0] {
		case '\r', '\n':
			return keyAccept
		case ' ':
			return keyToggle
		case 'a', 'A':
			return keyAll
		case 'n', 'N':
			return keyClear
		case 'q', 'Q', 3, 27: // q, Ctrl-C, bare ESC
			return keyCancel
		case 'k', 'K':
			return keyUp
		case 'j', 'J':
			return keyDown
		}
	case len(b) >= 3 && b[0] == 27 && b[1] == '[':
		switch b[2] {
		case 'A':
			return keyUp
		case 'B':
			return keyDown
		case 'C', 'D': // → and ← — intentionally inert for now
			return keyNoop
		}
	}
	return keyNoop
}

// frame renders the selector as plain text (the driver repaints it each key):
// an optional title, the capability checkboxes, an optional "Reject All" row,
// and a footer hint.
func (m *multiSelect) frame() string {
	var b strings.Builder
	if m.title != "" {
		fmt.Fprintf(&b, "%s\n", bold(m.title))
	}
	// Align descriptions to the widest label so the two columns line up.
	labelW := 0
	for _, it := range m.items {
		if len(it.label) > labelW {
			labelW = len(it.label)
		}
	}
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = cyan("▸ ")
		}
		box := "[ ]"
		if it.on {
			box = cyan("[x]")
		}
		line := fmt.Sprintf("%s%s %-*s", cursor, box, labelW, it.label)
		if it.desc != "" {
			line += "  " + dim(it.desc)
		}
		fmt.Fprintf(&b, "%s\n", line)
	}
	if m.reject {
		if m.cursor == len(m.items) {
			fmt.Fprintf(&b, "%s%s\n", cyan("▸ "), cyan("Reject All"))
		} else {
			fmt.Fprintf(&b, "  %s\n", dim("Reject All"))
		}
	}
	prompt := m.prompt
	if prompt == "" {
		prompt = "↑/↓ move · space toggle · enter submit · esc cancel"
	}
	fmt.Fprintf(&b, "%s\n", dim(prompt))
	return b.String()
}
