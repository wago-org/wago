package tui

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/ui"
)

// select.go is the dependency-free multi-select used for capability review.
// The model is pure (no terminal I/O); the shared raw-mode driver that feeds it
// keypresses and paints frames lives in select_unix.go / select_windows.go.

// selItem is one toggleable row.
type SelectItem struct {
	Label       string // machine value, e.g. a capability id "wasi:stdio"
	Description string // one-line human description (may be empty)
	Group       string // optional section heading shared by adjacent rows
	On          bool   // currently selected
	Disabled    bool   // visible preview row that cannot be toggled or selected
	Fixed       bool   // visible checked row that is required and cannot be changed
	Action      bool   // visible action row that submits instead of toggling
	Reject      bool   // exclusive visible action that clears every normal row
	ConfirmOff  bool   // submit immediately when this selected row is toggled off
}

// selectKey is a normalized keypress the model understands.
type selectKey int

type Key = selectKey

const (
	keyNoop selectKey = iota // unrecognized / intentionally inert (e.g. ← →)
	keyUp
	keyDown
	keyToggle // space
	keyAll    // a toggles the whole list
	keyClear  // n
	keyAccept // enter / return — submit the checked items
	keyReject // r — clear everything and submit (grant nothing)
	keyCancel // esc — abort or navigate back
	keyQuit   // q / ctrl-c — abort from any page
	keyLeft
	keyRight
	keyCopy // c
	keyOpen // o
)

const (
	KeyUp     = keyUp
	KeyDown   = keyDown
	KeyToggle = keyToggle
	KeyAccept = keyAccept
	KeyCancel = keyCancel
	KeyLeft   = keyLeft
	KeyRight  = keyRight
)

// Action is the result of a one-key action prompt.
type Action int

const (
	ActionNone Action = iota
	ActionContinue
	ActionCopy
	ActionOpen
)

// ActionPrompt presents compact keyboard actions without a selectable list.
// The model remains pure; callers perform the selected side effect after Run.
type ActionPrompt struct {
	Text   string
	Prompt string
	action Action
}

func (p *ActionPrompt) apply(key selectKey) (done, cancelled bool) {
	switch key {
	case keyCopy:
		p.action = ActionCopy
		return true, false
	case keyOpen:
		p.action = ActionOpen
		return true, false
	case keyAccept, keyRight:
		p.action = ActionContinue
		return true, false
	case keyLeft, keyCancel, keyQuit:
		return true, true
	}
	return false, false
}

func (p *ActionPrompt) frame() string {
	prompt := p.Prompt
	if prompt == "" {
		prompt = "c copy code · o open browser · enter continue · esc cancel"
	}
	return p.Text + "\n" + ui.Dim(prompt) + "\n"
}

// Action reports the submitted action after Run returns.
func (p *ActionPrompt) Action() Action { return p.action }

// Frame returns the prompt's rendered text for tests and previews.
func (p *ActionPrompt) Frame() string { return p.frame() }

// selectorModel is implemented by both the capability multi-select and the
// hierarchical single-choice picker.
type selectorModel interface {
	apply(selectKey) (done, cancelled bool)
	frame() string
}

// MultiSelect is the pure picker state: a list plus a cursor. Prompt overrides
// the default footer hint. A Reject item and the r key both submit rejection.
type MultiSelect struct {
	Title  string
	Prompt string
	Items  []SelectItem
	Cursor int
	// DisableRejectShortcut keeps cancellation as an explicit visible action.
	DisableRejectShortcut bool
}

// apply advances the model by one key. It reports whether the interaction is
// finished, and if so whether it was cancelled (esc) rather than submitted.
// Enter submits the focused action, r clears then submits rejection, movement
// clamps at the ends, and → is an alternate submit key.
func (m *MultiSelect) apply(k selectKey) (done, cancelled bool) {
	switch k {
	case keyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
	case keyDown:
		if m.Cursor < len(m.Items)-1 {
			m.Cursor++
		}
	case keyToggle:
		if len(m.Items) > 0 && !m.Items[m.Cursor].Disabled && !m.Items[m.Cursor].Fixed {
			if m.Items[m.Cursor].Reject {
				m.rejectAll()
				return true, false
			} else if m.Items[m.Cursor].Action {
				return true, false
			} else {
				m.Items[m.Cursor].On = !m.Items[m.Cursor].On
				if m.Items[m.Cursor].On {
					m.clearRejection()
				} else if m.Items[m.Cursor].ConfirmOff {
					return true, false
				}
			}
		}
	case keyAll:
		allSelected, selectable := true, false
		for _, item := range m.Items {
			if item.Disabled || item.Fixed || item.Action || item.Reject {
				continue
			}
			selectable = true
			if !item.On {
				allSelected = false
				break
			}
		}
		allSelected = allSelected && selectable
		for i := range m.Items {
			if !m.Items[i].Disabled && !m.Items[i].Fixed && !m.Items[i].Action && !m.Items[i].Reject {
				m.Items[i].On = !allSelected
			} else if m.Items[i].Reject {
				m.Items[i].On = false
			}
		}
	case keyClear:
		for i := range m.Items {
			if !m.Items[i].Disabled && !m.Items[i].Fixed && !m.Items[i].Action {
				m.Items[i].On = false
			}
		}
	case keyReject: // clear all, then submit — a deliberate "grant nothing"
		if m.DisableRejectShortcut {
			return false, false
		}
		m.rejectAll()
		return true, false
	case keyAccept, keyRight:
		if len(m.Items) > 0 && m.Items[m.Cursor].Reject && !m.Items[m.Cursor].Disabled {
			m.rejectAll()
		}
		return true, false
	case keyLeft, keyCancel, keyQuit:
		return true, true
	}
	return false, false
}

func (m *MultiSelect) rejectAll() {
	for i := range m.Items {
		m.Items[i].On = m.Items[i].Reject
	}
}

// chosen returns the labels of the selected rows, in list order.
func (m *MultiSelect) Chosen() []string {
	var out []string
	for _, it := range m.Items {
		if it.On && !it.Disabled && !it.Reject {
			out = append(out, it.Label)
		}
	}
	return out
}

// Rejected reports whether the explicit reject-all action is selected.
func (m *MultiSelect) Rejected() bool {
	for _, item := range m.Items {
		if item.Reject && item.On {
			return true
		}
	}
	return false
}

func (m *MultiSelect) clearRejection() {
	for i := range m.Items {
		if m.Items[i].Reject {
			m.Items[i].On = false
		}
	}
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
		case 'r', 'R':
			return keyReject
		case 'c', 'C':
			return keyCopy
		case 'o', 'O':
			return keyOpen
		case 27:
			return keyCancel
		case 'q', 'Q', 3:
			return keyQuit
		case 'k', 'K':
			return keyUp
		case 'j', 'J':
			return keyDown
		case 'h', 'H', '<':
			return keyLeft
		case 'l', 'L', '>':
			return keyRight
		}
	case len(b) >= 3 && b[0] == 27 && b[1] == '[':
		switch b[2] {
		case 'A':
			return keyUp
		case 'B':
			return keyDown
		case 'C':
			return keyRight
		case 'D':
			return keyLeft
		}
	}
	return keyNoop
}

// frame renders the selector as plain text (the driver repaints it each key):
// an optional title, radio-style selection marks, and a dim footer hint.
func (m *MultiSelect) frame() string {
	var b strings.Builder
	if m.Title != "" {
		fmt.Fprintf(&b, "%s\n", ui.Bold(m.Title))
	}
	// Align descriptions to the widest label so the two columns line up.
	labelW := 0
	for _, it := range m.Items {
		if len(it.Label) > labelW {
			labelW = len(it.Label)
		}
	}
	start, end := multiSelectWindow(m)
	if start > 0 {
		fmt.Fprintf(&b, "%s\n", ui.Dim(fmt.Sprintf("  ↑ %d more", start)))
	}
	previousGroup := ""
	for i := start; i < end; i++ {
		it := m.Items[i]
		if it.Group != "" && it.Group != previousGroup {
			fmt.Fprintf(&b, "%s\n", ui.Bold(it.Group))
		}
		previousGroup = it.Group
		cursor := "  "
		if it.Group != "" {
			cursor = "    "
		}
		if i == m.Cursor {
			cursor = ui.Cyan("› ")
			if it.Group != "" {
				cursor = ui.Cyan("  › ")
			}
		}
		if it.Action {
			line := fmt.Sprintf("%s[ %s ]", cursor, it.Label)
			if it.Description != "" {
				line += "  " + ui.Dim(it.Description)
			}
			fmt.Fprintf(&b, "%s\n", line)
			continue
		}
		mark := "○"
		if it.Disabled {
			mark = ui.Dim("◌")
		} else if it.Fixed {
			mark = ui.Cyan("✓")
		} else if it.On {
			mark = ui.Cyan("◉")
		} else if i == m.Cursor {
			mark = ui.Cyan(mark)
		}
		label := it.Label
		if it.Disabled {
			label = ui.Dim(label)
		}
		line := fmt.Sprintf("%s%s %-*s", cursor, mark, labelW, label)
		if it.Description != "" {
			line += "  " + ui.Dim(it.Description)
		}
		fmt.Fprintf(&b, "%s\n", line)
	}
	if remaining := len(m.Items) - end; remaining > 0 {
		fmt.Fprintf(&b, "%s\n", ui.Dim(fmt.Sprintf("  ↓ %d more", remaining)))
	}
	prompt := m.Prompt
	if prompt == "" {
		prompt = "↑/↓ move · space toggle · enter/→ accept · r reject all · ←/esc cancel"
	}
	fmt.Fprintf(&b, "%s\n", ui.Dim(prompt))
	return b.String()
}

func (m *MultiSelect) Frame() string {
	return m.frame()
}

func multiSelectWindow(m *MultiSelect) (start, end int) {
	if len(m.Items) <= pickerVisibleRows {
		return 0, len(m.Items)
	}
	start = (m.Cursor / pickerVisibleRows) * pickerVisibleRows
	end = start + pickerVisibleRows
	if end > len(m.Items) {
		end = len(m.Items)
	}
	return start, end
}
